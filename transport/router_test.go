package transport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
)

func TestStandaloneRouterExposesOnlyCoreRoutes(t *testing.T) {
	db, err := store.OpenWithOptions(store.Options{
		Driver: store.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "ztools.db"),
	})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	authService := auth.New(db, "standalone-test-secret")
	if _, err := authService.EnsureUser("owner", "strong-password"); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	syncService := syncsvc.NewServiceWithOptions(repository.New(db), syncsvc.ServiceOptions{
		MaxConcurrentPushes: 1,
	})
	handler := NewRouter(authService, syncService, NewHub(), RouterOptions{
		AllowRegistration: false,
	}).Handler()

	for _, path := range []string{"/api/register", "/api/market/plugins", "/api/console/admin/users"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("expected %s to be unavailable, got %d", path, response.Code)
		}
	}

	loginBody, _ := json.Marshal(map[string]string{"uid": "owner", "password": "strong-password"})
	request := httptest.NewRequest(http.MethodPost, "/api/auth", bytes.NewReader(loginBody))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("owner login failed: status=%d body=%s", response.Code, response.Body.String())
	}

	unknownBody, _ := json.Marshal(map[string]string{"uid": "unknown", "password": "password"})
	request = httptest.NewRequest(http.MethodPost, "/api/auth", bytes.NewReader(unknownBody))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("standalone auth created an unknown user: status=%d", response.Code)
	}
}
