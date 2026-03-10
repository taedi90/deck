package store

import "regexp"

var (
	recordIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	nodeIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

type Store struct {
	root string
}

type Release struct {
	ID           string `json:"id"`
	BundleSHA256 string `json:"bundle_sha256,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

type Session struct {
	ID        string `json:"id"`
	ReleaseID string `json:"release_id,omitempty"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at,omitempty"`
	ClosedAt  string `json:"closed_at,omitempty"`
}

type Node struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname,omitempty"`
}

type Assignment struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	NodeID    string `json:"node_id"`
	Role      string `json:"role,omitempty"`
	Workflow  string `json:"workflow,omitempty"`
	Status    string `json:"status,omitempty"`
}

type ExecutionReport struct {
	ID           string `json:"id"`
	SessionID    string `json:"session_id"`
	AssignmentID string `json:"assignment_id,omitempty"`
	NodeID       string `json:"node_id"`
	Hostname     string `json:"hostname,omitempty"`
	Action       string `json:"action,omitempty"`
	WorkflowRef  string `json:"workflow_ref,omitempty"`
	Status       string `json:"status,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	EndedAt      string `json:"ended_at,omitempty"`
}

type SessionNodeActionStatus struct {
	Diff   string `json:"diff"`
	Doctor string `json:"doctor"`
	Apply  string `json:"apply"`
}

type SessionStatusNode struct {
	NodeID   string                  `json:"node_id"`
	Hostname string                  `json:"hostname,omitempty"`
	Actions  SessionNodeActionStatus `json:"actions"`
}

type SessionStatusBucket struct {
	OK      []string `json:"ok"`
	Failed  []string `json:"failed"`
	Skipped []string `json:"skipped"`
	NotRun  []string `json:"not_run"`
}

type SessionStatusGroups struct {
	Diff   SessionStatusBucket `json:"diff"`
	Doctor SessionStatusBucket `json:"doctor"`
	Apply  SessionStatusBucket `json:"apply"`
}

type SessionStatusAggregation struct {
	Nodes  map[string]SessionStatusNode `json:"nodes"`
	Groups SessionStatusGroups          `json:"groups"`
}
