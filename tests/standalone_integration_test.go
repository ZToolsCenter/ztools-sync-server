package tests

import (
	"bytes"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
	"github.com/ZToolsCenter/ztools-sync-server/transport"
)

func TestStandaloneMySQLUsesMinimalSchema(t *testing.T) {
	if !mysqlReachable() {
		t.Skip("mysql is not reachable")
	}
	dbName := fmt.Sprintf("ztools_standalone_test_%d", time.Now().UnixNano())
	admin, err := sql.Open("mysql", mysqlAdminDSN())
	if err != nil {
		t.Fatalf("open mysql admin: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create standalone mysql database: %v", err)
	}
	defer admin.Exec("DROP DATABASE `" + dbName + "`")

	db, err := store.OpenWithOptions(store.Options{
		Driver: store.DriverMySQL,
		DSN:    mysqlTestDSN(dbName),
	})
	if err != nil {
		t.Fatalf("open standalone mysql store: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if !db.Migrator().HasTable("documents") || !db.Migrator().HasTable("attachment_blobs") {
		t.Fatal("standalone mysql schema is missing sync tables")
	}
	if db.Migrator().HasTable("plugins") || db.Migrator().HasTable("notifications") {
		t.Fatal("standalone mysql schema contains SaaS tables")
	}
}

func setupStandaloneServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, err := store.OpenWithOptions(store.Options{
		Driver: store.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "ztools.db"),
	})
	if err != nil {
		t.Fatalf("open standalone store: %v", err)
	}
	authService := auth.New(db, "standalone-integration-secret")
	if _, err := authService.EnsureUser("owner", "pass123"); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	syncService := syncsvc.NewServiceWithOptions(repository.New(db), syncsvc.ServiceOptions{
		MaxConcurrentPushes: 1,
	})
	router := transport.NewRouter(authService, syncService, transport.NewHub(), transport.RouterOptions{
		AllowRegistration: false,
	})
	server := httptest.NewServer(router.Handler())
	t.Cleanup(func() {
		server.Close()
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	return server
}

func TestStandaloneSQLiteSyncContract(t *testing.T) {
	server := setupStandaloneServer(t)
	status, login := postJSON(t, server.URL, "/api/auth", map[string]string{
		"uid": "owner", "password": "pass123",
	}, "")
	if status != http.StatusOK {
		t.Fatalf("standalone login failed: status=%d body=%#v", status, login)
	}
	token, _ := login["token"].(string)
	if token == "" {
		t.Fatal("standalone login returned no token")
	}

	client := dialWS(t, server.URL, token, "standalone-device")
	defer client.Close()
	if err := client.WriteJSON(map[string]any{
		"type":            "push",
		"protocolVersion": 2,
		"changes": []any{
			revisionChange("PLUGIN/standalone-doc", "1-initial", "", "standalone"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	push := readMessage(t, client, "push_ok")
	if push["seq"].(float64) <= 0 {
		t.Fatalf("standalone push returned invalid seq: %#v", push)
	}

	if err := client.WriteJSON(map[string]any{
		"type": "put_checkpoint",
		"checkpoint": map[string]any{
			"id": "standalone-checkpoint", "sourceId": "local", "targetId": "standalone", "lastSeq": push["seq"],
		},
	}); err != nil {
		t.Fatal(err)
	}
	readMessage(t, client, "checkpoint_ok")

	blobBody := []byte("standalone attachment")
	sum := md5.Sum(blobBody)
	digest := "md5-" + hex.EncodeToString(sum[:])
	doc, _ := json.Marshal(map[string]any{
		"_id":  "PLUGIN/standalone-attachment",
		"_rev": "1-attachment",
		"_attachments": map[string]any{
			"file.txt": map[string]any{
				"stub": true, "digest": digest, "content_type": "text/plain", "length": len(blobBody), "revpos": 1,
			},
		},
	})
	change := syncsvc.FullChangeEntry{
		DocID: "PLUGIN/standalone-attachment", Rev: "1-attachment", Doc: doc,
	}
	if err := client.WriteJSON(map[string]any{"type": "push", "protocolVersion": 2, "changes": []any{change}}); err != nil {
		t.Fatal(err)
	}
	missing := readMessage(t, client, "push_missing")
	if len(missing["missingDigests"].([]any)) != 1 {
		t.Fatalf("expected missing attachment digest: %#v", missing)
	}

	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/sync/attachments/blobs/"+digest, bytes.NewReader(blobBody))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "text/plain")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("standalone attachment upload returned %d", response.StatusCode)
	}

	if err := client.WriteJSON(map[string]any{"type": "push", "protocolVersion": 2, "changes": []any{change}}); err != nil {
		t.Fatal(err)
	}
	readMessage(t, client, "push_ok")

	request, _ = http.NewRequest(http.MethodGet, server.URL+"/api/sync/attachments/blobs/"+digest, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	got, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !bytes.Equal(got, blobBody) {
		t.Fatalf("standalone attachment round trip failed: status=%d body=%q", response.StatusCode, got)
	}
}
