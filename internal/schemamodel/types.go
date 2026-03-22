package schemamodel

type WorkflowDocument struct {
	Version string            `json:"version"`
	Vars    map[string]any    `json:"vars,omitempty"`
	Phases  []WorkflowPhase   `json:"phases,omitempty"`
	Steps   []WorkflowStepRef `json:"steps,omitempty"`
}

type WorkflowPhase struct {
	Name           string            `json:"name"`
	MaxParallelism int               `json:"maxParallelism,omitempty"`
	Imports        []WorkflowImport  `json:"imports,omitempty"`
	Steps          []WorkflowStepRef `json:"steps,omitempty"`
}

type WorkflowImport struct {
	Path string `json:"path"`
	When string `json:"when,omitempty"`
}

type WorkflowStepRef struct {
	ID            string            `json:"id"`
	APIVersion    string            `json:"apiVersion,omitempty"`
	Kind          string            `json:"kind"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
	When          string            `json:"when,omitempty"`
	ParallelGroup string            `json:"parallelGroup,omitempty"`
	Register      map[string]string `json:"register,omitempty"`
	Retry         int               `json:"retry,omitempty"`
	Timeout       string            `json:"timeout,omitempty"`
	Spec          map[string]any    `json:"spec"`
}

type ToolDefinitionDocument struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   ToolDefinitionMetadata `json:"metadata"`
	Spec       ToolDefinitionSpec     `json:"spec"`
}

type ToolDefinitionMetadata struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels,omitempty"`
	Description string            `json:"description,omitempty"`
}

type ToolDefinitionSpec struct {
	Version        string                        `json:"version"`
	Summary        string                        `json:"summary"`
	Category       string                        `json:"category"`
	OfflineAllowed bool                          `json:"offlineAllowed,omitempty"`
	Requires       *ToolDefinitionRequires       `json:"requires,omitempty"`
	InputSchema    map[string]any                `json:"inputSchema"`
	OutputContract *ToolDefinitionOutputContract `json:"outputContract,omitempty"`
	Idempotency    *ToolDefinitionIdempotency    `json:"idempotency,omitempty"`
	FailurePolicy  *ToolDefinitionFailurePolicy  `json:"failurePolicy,omitempty"`
}

type ToolDefinitionRequires struct {
	Network          bool   `json:"network,omitempty"`
	Root             bool   `json:"root,omitempty"`
	ContainerRuntime string `json:"containerRuntime,omitempty"`
}

type ToolDefinitionOutputContract struct {
	Artifact  []string `json:"artifacts,omitempty"`
	StateKeys []string `json:"stateKeys,omitempty"`
}

type ToolDefinitionIdempotency struct {
	Mode string   `json:"mode"`
	Keys []string `json:"keys"`
}

type ToolDefinitionFailurePolicy struct {
	Retryable    bool `json:"retryable,omitempty"`
	DefaultRetry int  `json:"defaultRetry,omitempty"`
}
