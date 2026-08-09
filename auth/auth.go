package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/ZToolsCenter/ztools-sync-server/models"
)

type Service struct {
	db            *gorm.DB
	jwtSecret     []byte
	userValidator func(string) (int64, error)
}

type Tokens struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
}

func New(db *gorm.DB, jwtSecret string) *Service {
	return &Service{db: db, jwtSecret: []byte(jwtSecret)}
}

// SetUserValidator installs an optional account-state check used before issuing
// or accepting access tokens. Its version is embedded in newly issued tokens,
// allowing callers to permanently invalidate older access tokens.
func (s *Service) SetUserValidator(validator func(string) (int64, error)) {
	s.userValidator = validator
}

func (s *Service) CreateUser(uid string, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return err
	}
	return s.db.Create(&models.User{
		UID:          uid,
		Nickname:     uid, // 默认昵称为用户名
		PasswordHash: string(hash),
		CreatedAt:    time.Now().UnixMilli(),
	}).Error
}

// EnsureUser creates the bootstrap owner only when the account does not already exist.
func (s *Service) EnsureUser(uid string, password string) (bool, error) {
	var user models.User
	err := s.db.First(&user, "uid = ?", uid).Error
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if err := s.CreateUser(uid, password); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) HasUsers() (bool, error) {
	var count int64
	if err := s.db.Model(&models.User{}).Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Service) Login(uid string, password string) (Tokens, error) {
	var user models.User
	if err := s.db.First(&user, "uid = ?", uid).Error; err != nil {
		return Tokens{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return Tokens{}, errors.New("invalid credentials")
	}
	return s.IssueTokens(uid)
}

func (s *Service) AuthOrCreate(uid string, password string) (Tokens, bool, error) {
	var user models.User
	err := s.db.First(&user, "uid = ?", uid).Error
	isNew := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := s.CreateUser(uid, password); err != nil {
			return Tokens{}, false, err
		}
		isNew = true
	} else if err != nil {
		return Tokens{}, false, err
	}
	tokens, err := s.Login(uid, password)
	return tokens, isNew, err
}

func (s *Service) Sign(uid string) (string, error) {
	var authVersion int64
	if s.userValidator != nil {
		var err error
		authVersion, err = s.userValidator(uid)
		if err != nil {
			return "", err
		}
	}
	claims := jwt.MapClaims{
		"uid": uid,
		"ver": authVersion,
		"exp": time.Now().Add(48 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func (s *Service) IssueTokens(uid string) (Tokens, error) {
	token, err := s.Sign(uid)
	if err != nil {
		return Tokens{}, err
	}
	refreshToken, err := generateOpaqueToken()
	if err != nil {
		return Tokens{}, err
	}
	now := time.Now().UnixMilli()
	if err := s.db.Create(&models.RefreshToken{
		UID:       uid,
		TokenHash: hashRefreshToken(refreshToken),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour).UnixMilli(),
		CreatedAt: now,
	}).Error; err != nil {
		return Tokens{}, err
	}
	return Tokens{Token: token, RefreshToken: refreshToken}, nil
}

func (s *Service) Refresh(refreshToken string) (Tokens, error) {
	hash := hashRefreshToken(refreshToken)
	var row models.RefreshToken
	if err := s.db.First(&row, "token_hash = ?", hash).Error; err != nil {
		return Tokens{}, err
	}
	now := time.Now().UnixMilli()
	if row.ExpiresAt <= now || row.UsedAt > 0 || row.RevokedAt > 0 {
		return Tokens{}, errors.New("invalid refresh token")
	}
	var tokens Tokens
	err := s.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.RefreshToken{}).
			Where("id = ? AND used_at = 0 AND revoked_at = 0 AND expires_at > ?", row.ID, now).
			Update("used_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("invalid refresh token")
		}
		token, err := s.Sign(row.UID)
		if err != nil {
			return err
		}
		nextRefreshToken, err := generateOpaqueToken()
		if err != nil {
			return err
		}
		if err := tx.Create(&models.RefreshToken{
			UID:       row.UID,
			TokenHash: hashRefreshToken(nextRefreshToken),
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour).UnixMilli(),
			CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		tokens = Tokens{Token: token, RefreshToken: nextRefreshToken}
		return nil
	})
	return tokens, err
}

func (s *Service) Verify(tokenValue string) (string, error) {
	token, err := jwt.Parse(tokenValue, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return "", errors.New("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims")
	}
	uid, ok := claims["uid"].(string)
	if !ok || uid == "" {
		return "", errors.New("missing uid")
	}
	tokenVersion, err := claimInt64(claims, "ver")
	if err != nil {
		return "", err
	}
	if s.userValidator != nil {
		currentVersion, err := s.userValidator(uid)
		if err != nil {
			return "", err
		}
		if tokenVersion != currentVersion {
			return "", errors.New("invalid token version")
		}
	} else {
		var user models.User
		if err := s.db.First(&user, "uid = ?", uid).Error; err != nil {
			return "", err
		}
		if tokenVersion != 0 {
			return "", errors.New("invalid token version")
		}
	}
	return uid, nil
}

func claimInt64(claims jwt.MapClaims, key string) (int64, error) {
	value, ok := claims[key]
	if !ok {
		return 0, nil
	}
	number, ok := value.(float64)
	if !ok || number < 0 || number != float64(int64(number)) {
		return 0, errors.New("invalid " + key + " claim")
	}
	return int64(number), nil
}

func generateOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
