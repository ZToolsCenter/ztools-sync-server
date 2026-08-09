package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestUserValidatorInvalidatesTokenWhenVersionChanges(t *testing.T) {
	version := int64(3)
	service := New(nil, "test-secret")
	service.SetUserValidator(func(uid string) (int64, error) {
		if uid != "alice" {
			t.Fatalf("unexpected uid: %s", uid)
		}
		return version, nil
	})

	token, err := service.Sign("alice")
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	if uid, err := service.Verify(token); err != nil || uid != "alice" {
		t.Fatalf("verify current token: uid=%q err=%v", uid, err)
	}

	version++
	if _, err := service.Verify(token); err == nil {
		t.Fatal("expected token to be rejected after auth version changed")
	}
}

func TestUserValidatorAcceptsLegacyTokenAtVersionZero(t *testing.T) {
	version := int64(0)
	service := New(nil, "test-secret")
	service.SetUserValidator(func(string) (int64, error) {
		return version, nil
	})

	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": "alice",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	token, err := legacy.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign legacy token: %v", err)
	}
	if uid, err := service.Verify(token); err != nil || uid != "alice" {
		t.Fatalf("verify legacy token: uid=%q err=%v", uid, err)
	}

	version = 1
	if _, err := service.Verify(token); err == nil {
		t.Fatal("expected legacy token to be rejected after auth version changed")
	}
}
