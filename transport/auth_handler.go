package transport

import (
	"net/http"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
)

type AuthHandler struct {
	auth              *auth.Service
	allowRegistration bool
}

func NewAuthHandler(authService *auth.Service, allowRegistration bool) *AuthHandler {
	return &AuthHandler{auth: authService, allowRegistration: allowRegistration}
}

type authRequest struct {
	UID                string `json:"uid"`
	Password           string `json:"password"`
	CaptchaVerifyParam string `json:"captchaVerifyParam"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (h *AuthHandler) Auth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	var req authRequest
	if err := readJSON(r, &req); err != nil || req.UID == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uid and password required"})
		return
	}
	if err := h.verifyCaptcha(req.CaptchaVerifyParam); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var tokens auth.Tokens
	isNew := false
	var err error
	if h.allowRegistration {
		tokens, isNew, err = h.auth.AuthOrCreate(req.UID, req.Password)
	} else {
		tokens, err = h.auth.Login(req.UID, req.Password)
	}
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "密码错误"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"token": tokens.Token, "refreshToken": tokens.RefreshToken, "isNew": isNew})
}

func (h *AuthHandler) CaptchaConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.allowRegistration {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	var req authRequest
	if err := readJSON(r, &req); err != nil || req.UID == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uid and password required"})
		return
	}
	if err := h.verifyCaptcha(req.CaptchaVerifyParam); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.auth.CreateUser(req.UID, req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tokens, err := h.auth.Login(req.UID, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tokens.Token, "refreshToken": tokens.RefreshToken})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	var req authRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.UID == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "uid and password required"})
		return
	}
	if err := h.verifyCaptcha(req.CaptchaVerifyParam); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tokens, err := h.auth.Login(req.UID, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tokens.Token, "refreshToken": tokens.RefreshToken})
}

func (h *AuthHandler) verifyCaptcha(captchaVerifyParam string) error {
	return nil
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Not found"})
		return
	}
	var req refreshRequest
	if err := readJSON(r, &req); err != nil || req.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refreshToken required"})
		return
	}
	tokens, err := h.auth.Refresh(req.RefreshToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid refresh token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tokens.Token, "refreshToken": tokens.RefreshToken})
}
