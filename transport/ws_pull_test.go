package transport

import (
	"bytes"
	"context"
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

func TestStreamedPullPreservesLegacyEnvelope(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			changes := make([]syncsvc.FullChangeEntry, 0)
			if !empty {
				parent := "1-root"
				changes = append(changes,
					syncsvc.FullChangeEntry{Seq: 1, DocID: "doc", Rev: "1-root", Doc: json.RawMessage(`{"text":"中文 <>&"}`), RevisionHistory: []string{"1-root"}},
					syncsvc.FullChangeEntry{Seq: 2, DocID: "doc", Rev: "2-deleted", ParentRev: &parent, Deleted: true, RevisionHistory: []string{"2-deleted", "1-root"}},
				)
			}
			message := syncsvc.ServerMessage{Type: "changes", Seq: 42, SyncEpoch: 9, Snapshot: snapshot, Reset: snapshot, Changes: changes}
			var output bytes.Buffer
			stats, err := encodePullResponse(context.Background(), &output, message, func(consume syncsvc.ChangeConsumer) (int64, error) {
				for _, change := range changes {
					if err := consume(change); err != nil {
						return 0, err
					}
				}
				return 42, nil
			})
			if err != nil || stats.changes != len(changes) || stats.bytes != int64(output.Len()) {
				t.Fatalf("stream failed: %+v %v", stats, err)
			}
			want, _ := json.Marshal(message)
			var actual, expected any
			if err := json.Unmarshal(output.Bytes(), &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("legacy envelope changed: got=%s want=%s", output.Bytes(), want)
			}
		}
	}
}

func TestStreamFailureNeverCompletesJSON(t *testing.T) {
	var output bytes.Buffer
	wantErr := errors.New("query failed after the first page")
	_, err := encodePullResponse(context.Background(), &output, syncsvc.ServerMessage{Type: "changes"}, func(consume syncsvc.ChangeConsumer) (int64, error) {
		if err := consume(syncsvc.FullChangeEntry{DocID: "first", Doc: json.RawMessage("{}")}); err != nil {
			return 0, err
		}
		return 999, wantErr
	})
	if !errors.Is(err, wantErr) || json.Valid(output.Bytes()) || bytes.Contains(output.Bytes(), []byte(`"seq":999`)) {
		t.Fatalf("partial response became a checkpoint acknowledgment: %s %v", output.Bytes(), err)
	}
	if _, err := encodePullResponse(context.Background(), failingChangesWriter{}, syncsvc.ServerMessage{}, func(syncsvc.ChangeConsumer) (int64, error) {
		t.Fatal("writer failure did not stop the query")
		return 0, nil
	}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("writer error lost: %v", err)
	}
}

func TestPullGenerationCanceledOnDisconnectOrTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		name := "disconnect"
		if timeout {
			name = "timeout"
		}
		t.Run(name, func(t *testing.T) {
			result := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					result <- err
					return
				}
				peer := newWebSocketConnection(conn)
				defer peer.Close()
				ctx, cancelRead, _ := peer.readMessages(r.Context())
				defer cancelRead()
				if timeout {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
					defer cancel()
				}
				_, err = peer.writePull(ctx, syncsvc.ServerMessage{Type: "changes"}, func(consume syncsvc.ChangeConsumer) (int64, error) {
					body := json.RawMessage(`{"text":"` + strings.Repeat("x", 16*1024) + `"}`)
					if err := consume(syncsvc.FullChangeEntry{DocID: "doc", Doc: body}); err != nil {
						return 0, err
					}
					<-ctx.Done()
					return 0, ctx.Err()
				})
				result <- err
			}))
			defer server.Close()
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, reader, err := client.NextReader()
			if err != nil {
				t.Fatal(err)
			}
			prefix := make([]byte, 16)
			if _, err := io.ReadFull(reader, prefix); err != nil {
				t.Fatal(err)
			}
			if !timeout {
				_ = client.Close()
			} else if _, err := io.ReadAll(reader); err == nil {
				t.Fatal("timeout finalized an incomplete response")
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("generation was not canceled: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("disconnected pull kept running")
			}
		})
	}
}
