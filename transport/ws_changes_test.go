package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
	"github.com/gorilla/websocket"
)

func TestEncodedChangesPreserveJSONContract(t *testing.T) {
	for _, changes := range [][]syncsvc.FullChangeEntry{
		nil,
		{},
		{{DocID: "doc", Rev: "1-a", Doc: json.RawMessage(`{"text":"中文 <>&"}`), RevisionHistory: []string{"1-a"}}, {DocID: "gone", Rev: "2-b", Deleted: true}},
	} {
		message := syncsvc.ServerMessage{Type: "changes", Changes: changes, Seq: 42, SyncEpoch: 7, Snapshot: true, Reset: true}
		var output bytes.Buffer
		if err := encodeChanges(&output, message); err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		var actualJSON, expectedJSON any
		if err := json.Unmarshal(output.Bytes(), &actualJSON); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if err := json.Unmarshal(want, &expectedJSON); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actualJSON, expectedJSON) {
			t.Fatalf("wire response changed: got=%s want=%s", output.Bytes(), want)
		}
		if !reflect.DeepEqual(message.Changes, changes) {
			t.Fatal("encoding modified the caller's response")
		}
	}
}

func TestWriteChangesSendsOneMessageAndAbortsInvalidJSON(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmtBool(invalid), func(t *testing.T) {
			doc := json.RawMessage(`{"text":"` + strings.Repeat("x", 16*1024) + `"}`)
			message := syncsvc.ServerMessage{Type: "changes", Changes: []syncsvc.FullChangeEntry{{Doc: doc}, {Doc: doc}}, Seq: 123}
			if invalid {
				message.Changes[1].Doc = json.RawMessage(`{"incomplete":`)
			}
			result := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					result <- err
					return
				}
				defer conn.Close()
				peer := newWebSocketConnection(conn)
				if err := peer.WriteChanges(message); err != nil {
					result <- err
					return
				}
				result <- peer.WriteJSON(syncsvc.ServerMessage{Type: "pong"})
			}))
			defer server.Close()
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
			var actual syncsvc.ServerMessage
			err = client.ReadJSON(&actual)
			if invalid {
				if err == nil || <-result == nil {
					t.Fatal("partial response was accepted as a complete message")
				}
				return
			}
			if err != nil || !reflect.DeepEqual(actual, message) {
				t.Fatalf("fragmented response did not reassemble: %v", err)
			}
			if err := client.ReadJSON(&actual); err != nil || actual.Type != "pong" {
				t.Fatalf("response ended before all entries or swallowed next message: %v", err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func fmtBool(invalid bool) string {
	if invalid {
		return "abort_invalid_body"
	}
	return "complete_message"
}

type failingChangesWriter struct{}

func (failingChangesWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestEncodeChangesReturnsWriteFailure(t *testing.T) {
	err := encodeChanges(failingChangesWriter{}, syncsvc.ServerMessage{Type: "changes", Changes: []syncsvc.FullChangeEntry{}})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write failure lost: %v", err)
	}
}

func BenchmarkChangesEncoding(b *testing.B) {
	message := syncsvc.ServerMessage{Type: "changes", Changes: make([]syncsvc.FullChangeEntry, 256), Seq: 256}
	doc := json.RawMessage(`{"text":"` + strings.Repeat("x", 64*1024) + `"}`)
	for index := range message.Changes {
		message.Changes[index] = syncsvc.FullChangeEntry{DocID: "doc", Rev: "1-a", Doc: doc}
	}
	b.Run("whole_response", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			if err := json.NewEncoder(io.Discard).Encode(message); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("one_entry_at_a_time", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			if err := encodeChanges(io.Discard, message); err != nil {
				b.Fatal(err)
			}
		}
	})
}
