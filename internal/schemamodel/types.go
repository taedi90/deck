package schemamodel

type WorkflowDocument struct {
	Role      string            `json:"role"`
	Version   string            `json:"version"`
	Vars      map[string]any    `json:"vars,omitempty"`
	Artifacts *ArtifactsSpec    `json:"artifacts,omitempty"`
	Phases    []WorkflowPhase   `json:"phases,omitempty"`
	Steps     []WorkflowStepRef `json:"steps,omitempty"`
}

type ArtifactsSpec struct {
	Files    []ArtifactFileGroup    `json:"files,omitempty"`
	Images   []ArtifactImageGroup   `json:"images,omitempty"`
	Packages []ArtifactPackageGroup `json:"packages,omitempty"`
}

type ArtifactTarget struct {
	OS       string `json:"os,omitempty"`
	OSFamily string `json:"osFamily,omitempty"`
	Release  string `json:"release,omitempty"`
	Arch     string `json:"arch,omitempty"`
}

type ArtifactSource struct {
	URL    string `json:"url,omitempty"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type ArtifactFileOutput struct {
	Path string `json:"path"`
	Mode string `json:"mode,omitempty"`
}

type ArtifactFileItem struct {
	ID       string             `json:"id"`
	Source   ArtifactSource     `json:"source"`
	Output   ArtifactFileOutput `json:"output"`
	Checksum string             `json:"checksum,omitempty"`
	Mode     string             `json:"mode,omitempty"`
}

type ArtifactFileGroup struct {
	Group   string             `json:"group"`
	Targets []ArtifactTarget   `json:"targets,omitempty"`
	Items   []ArtifactFileItem `json:"items"`
}

type ArtifactImageItem struct {
	Image string `json:"image"`
}

type ArtifactImageGroup struct {
	Group   string              `json:"group"`
	Targets []ArtifactTarget    `json:"targets,omitempty"`
	Items   []ArtifactImageItem `json:"items"`
	Backend map[string]any      `json:"backend,omitempty"`
	Output  map[string]any      `json:"output,omitempty"`
}

type ArtifactPackageItem struct {
	Name string `json:"name"`
}

type ArtifactPackageGroup struct {
	Group   string                `json:"group"`
	Targets []ArtifactTarget      `json:"targets"`
	Items   []ArtifactPackageItem `json:"items"`
	Repo    map[string]any        `json:"repo,omitempty"`
	Backend map[string]any        `json:"backend,omitempty"`
}

type WorkflowPhase struct {
	Name    string            `json:"name"`
	Imports []WorkflowImport  `json:"imports,omitempty"`
	Steps   []WorkflowStepRef `json:"steps,omitempty"`
}

type WorkflowImport struct {
	Path string `json:"path"`
	When string `json:"when,omitempty"`
}

type WorkflowStepRef struct {
	ID         string            `json:"id"`
	APIVersion string            `json:"apiVersion,omitempty"`
	Kind       string            `json:"kind"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	When       string            `json:"when,omitempty"`
	Register   map[string]string `json:"register,omitempty"`
	Retry      int               `json:"retry,omitempty"`
	Timeout    string            `json:"timeout,omitempty"`
	Spec       map[string]any    `json:"spec"`
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
	Artifacts []string `json:"artifacts,omitempty"`
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
