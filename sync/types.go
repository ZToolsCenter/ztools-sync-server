package sync

import (
	"encoding/json"
)

type FullChangeEntry struct {
	Seq             int64           `json:"seq"`
	DocID           string          `json:"docId"`
	Rev             string          `json:"rev"`
	ParentRev       *string         `json:"parentRev,omitempty"`
	RevisionHistory []string        `json:"revisionHistory,omitempty"`
	Deleted         bool            `json:"deleted"`
	Timestamp       int64           `json:"timestamp"`
	Doc             json.RawMessage `json:"doc"`
	DocType         string          `json:"docType,omitempty"`
	WinnerRev       string          `json:"winnerRev,omitempty"`
	IsWinner        bool            `json:"isWinner,omitempty"`
	Resolution      *Resolution     `json:"resolution,omitempty"`
	ProtocolVersion int             `json:"protocolVersion,omitempty"`
}

type Resolution struct {
	RetireOtherLeaves bool `json:"retireOtherLeaves,omitempty"`
}

type CheckpointPayload struct {
	ID              string          `json:"id"`
	SourceID        string          `json:"sourceId,omitempty"`
	TargetID        string          `json:"targetId,omitempty"`
	LastSeq         int64           `json:"lastSeq"`
	RemotePullSeq   int64           `json:"remotePullSeq,omitempty"`
	LocalPushSeq    int64           `json:"localPushSeq,omitempty"`
	SyncEpoch       int64           `json:"syncEpoch,omitempty"`
	ProtocolVersion int             `json:"protocolVersion,omitempty"`
	UpdatedAt       int64           `json:"updatedAt,omitempty"`
	Data            json.RawMessage `json:"data,omitempty"`
}

type ClientMessage struct {
	Type            string             `json:"type"`
	Token           string             `json:"token,omitempty"`
	DeviceID        string             `json:"deviceId,omitempty"`
	DeviceName      string             `json:"deviceName,omitempty"`
	Since           int64              `json:"since,omitempty"`
	Snapshot        bool               `json:"snapshot,omitempty"`
	Checkpoint      *CheckpointPayload `json:"checkpoint,omitempty"`
	CheckpointID    string             `json:"checkpointId,omitempty"`
	Changes         []FullChangeEntry  `json:"changes,omitempty"`
	ProtocolVersion int                `json:"protocolVersion,omitempty"`
}

type ServerMessage struct {
	Type             string             `json:"type"`
	ServerInstanceID string             `json:"serverInstanceId,omitempty"`
	ServerSeq        int64              `json:"serverSeq"`
	ProtocolVersion  int                `json:"protocolVersion"`
	SyncEpoch        int64              `json:"syncEpoch"`
	Features         map[string]bool    `json:"features,omitempty"`
	Changes          []FullChangeEntry  `json:"changes"`
	Seq              int64              `json:"seq"`
	Snapshot         bool               `json:"snapshot,omitempty"`
	Reset            bool               `json:"reset,omitempty"`
	Change           *FullChangeEntry   `json:"change,omitempty"`
	Checkpoint       *CheckpointPayload `json:"checkpoint,omitempty"`
	Message          string             `json:"message,omitempty"`
	MissingDigests   []string           `json:"missingDigests,omitempty"`
}
