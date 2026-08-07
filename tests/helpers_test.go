package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
)

func mysqlAdminDSN() string {
	user := testEnv("MYSQL_USER", "root")
	password := os.Getenv("MYSQL_PASSWORD")
	host := testEnv("MYSQL_HOST", "127.0.0.1")
	port := testEnv("MYSQL_PORT", "3306")
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&loc=Local", user, password, host, port)
}

func mysqlTestDSN(dbName string) string {
	user := testEnv("MYSQL_USER", "root")
	password := os.Getenv("MYSQL_PASSWORD")
	host := testEnv("MYSQL_HOST", "127.0.0.1")
	port := testEnv("MYSQL_PORT", "3306")
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", user, password, host, port, dbName)
}

func mysqlReachable() bool {
	db, err := sql.Open("mysql", mysqlAdminDSN())
	if err != nil {
		return false
	}
	defer db.Close()
	return db.Ping() == nil
}

func testEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func postJSON(t *testing.T, baseURL string, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	request, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&result)
	return response.StatusCode, result
}

func dialWS(t *testing.T, serverURL string, token string, deviceID string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws"
	connection, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteJSON(map[string]any{
		"type":            "auth",
		"token":           token,
		"deviceId":        deviceID,
		"deviceName":      deviceID,
		"protocolVersion": 2,
	}); err != nil {
		t.Fatal(err)
	}
	readMessage(t, connection, "auth_ok")
	return connection
}

func readMessage(t *testing.T, connection *websocket.Conn, expected string) map[string]any {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	message := map[string]any{}
	if err := connection.ReadJSON(&message); err != nil {
		t.Fatal(err)
	}
	if message["type"] != expected {
		t.Fatalf("expected %s, got %#v", expected, message)
	}
	return message
}

func revisionChange(docID string, rev string, parentRev string, payload string) syncsvc.FullChangeEntry {
	doc, _ := json.Marshal(map[string]any{"_id": docID, "_rev": rev, "payload": payload})
	var parent *string
	if parentRev != "" {
		parent = &parentRev
	}
	return syncsvc.FullChangeEntry{
		DocID:     docID,
		Rev:       rev,
		ParentRev: parent,
		Timestamp: time.Now().UnixMilli(),
		Doc:       doc,
	}
}
