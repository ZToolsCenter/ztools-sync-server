package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
	"github.com/gorilla/websocket"
)

func TestLargeBacklogSnapshotPreservesV1AndV2Clients(t *testing.T) {
	server := setupStandaloneServer(t)
	status, login := postJSON(t, server.URL, "/api/auth", map[string]string{"uid": "owner", "password": "pass123"}, "")
	if status != http.StatusOK {
		t.Fatal("fixture login failed")
	}
	token := login["token"].(string)
	client := dialWS(t, server.URL, token, "v2-reader")
	defer client.Close()
	changes := make([]syncsvc.FullChangeEntry, 0, 604)
	for i := 1; i <= 600; i++ {
		parent := ""
		if i > 1 {
			parent = fmt.Sprintf("%d-chain", i-1)
		}
		changes = append(changes, revisionChange("doc", fmt.Sprintf("%d-chain", i), parent, "body"))
	}
	changes = append(changes,
		revisionChange("doc", "601-left", "600-chain", "left"),
		revisionChange("doc", "601-right", "600-chain", "right"),
		revisionChange("gone", "1-original", "", "delete me"),
		syncsvc.FullChangeEntry{DocID: "gone", Rev: "2-deleted", ParentRev: stringPointer("1-original"), Deleted: true},
	)
	if err := client.WriteJSON(map[string]any{"type": "push", "changes": changes, "protocolVersion": 2}); err != nil {
		t.Fatal(err)
	}
	seq := readMessage(t, client, "push_ok")["seq"]
	if err := client.WriteJSON(map[string]any{"type": "pull", "since": 0, "protocolVersion": 2}); err != nil {
		t.Fatal(err)
	}
	response := readMessage(t, client, "changes")
	if response["snapshot"] != true || response["reset"] != true || response["seq"] != seq {
		t.Fatalf("large pull did not use the existing snapshot envelope: %#v", response)
	}
	leaves := response["changes"].([]any)
	if len(leaves) != 3 {
		t.Fatalf("snapshot dropped current branches or tombstones: %d", len(leaves))
	}
	for _, raw := range leaves {
		change := raw.(map[string]any)
		history := change["revisionHistory"].([]any)
		if change["docId"] == "doc" {
			if len(history) != 601 || history[600] != "1-chain" || change["winnerRev"] != "601-right" {
				t.Fatalf("snapshot lost complete ancestry or winner: rev=%v ancestors=%d", change["rev"], len(history))
			}
		} else if change["docId"] != "gone" || change["deleted"] != true || change["doc"] != nil || len(history) != 2 {
			t.Fatalf("snapshot tombstone changed: %#v", change)
		}
	}
	if err := client.WriteJSON(map[string]any{"type": "get_checkpoint", "checkpointId": "not-acknowledged"}); err != nil {
		t.Fatal(err)
	}
	checkpoint := readMessage(t, client, "checkpoint")["checkpoint"].(map[string]any)
	if checkpoint["lastSeq"] != float64(0) {
		t.Fatal("server advanced an unacknowledged checkpoint")
	}

	// A v1 client continues to receive the original complete incremental history.
	legacy, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if err := legacy.WriteJSON(map[string]any{"type": "auth", "token": token, "deviceId": "v1-reader", "protocolVersion": 1}); err != nil {
		t.Fatal(err)
	}
	readMessage(t, legacy, "auth_ok")
	if err := legacy.WriteJSON(map[string]any{"type": "pull", "since": 0, "protocolVersion": 1}); err != nil {
		t.Fatal(err)
	}
	oldResponse := readMessage(t, legacy, "changes")
	if oldResponse["snapshot"] == true || len(oldResponse["changes"].([]any)) != len(changes) || oldResponse["seq"] != seq {
		t.Fatalf("v1 history changed after a v2 snapshot: entries=%d", len(oldResponse["changes"].([]any)))
	}
}

func stringPointer(value string) *string { return &value }
