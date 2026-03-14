package config

type Workflow struct {
	Role           string         `yaml:"role" json:"-"`
	Version        string         `yaml:"version" json:"version"`
	Imports        []string       `yaml:"imports,omitempty" json:"imports,omitempty"`
	VarImports     []string       `yaml:"varImports,omitempty" json:"varImports,omitempty"`
	Vars           map[string]any `yaml:"vars" json:"vars,omitempty"`
	Artifacts      *ArtifactsSpec `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	Phases         []Phase        `yaml:"phases,omitempty" json:"phases,omitempty"`
	Steps          []Step         `yaml:"steps,omitempty" json:"-"`
	StateKey       string         `yaml:"-" json:"-"`
	WorkflowSHA256 string         `yaml:"-" json:"-"`
}

type ArtifactsSpec struct {
	Files    []ArtifactFileGroup    `yaml:"files,omitempty" json:"files,omitempty"`
	Images   []ArtifactImageGroup   `yaml:"images,omitempty" json:"images,omitempty"`
	Packages []ArtifactPackageGroup `yaml:"packages,omitempty" json:"packages,omitempty"`
}

type ArtifactTarget struct {
	OS       string `yaml:"os,omitempty" json:"os,omitempty"`
	OSFamily string `yaml:"osFamily,omitempty" json:"osFamily,omitempty"`
	Release  string `yaml:"release,omitempty" json:"release,omitempty"`
	Arch     string `yaml:"arch,omitempty" json:"arch,omitempty"`
}

type ArtifactSource struct {
	URL    string `yaml:"url,omitempty" json:"url,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	SHA256 string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
}

type ArtifactFileOutput struct {
	Path string `yaml:"path" json:"path"`
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type ArtifactFileItem struct {
	ID       string             `yaml:"id" json:"id"`
	Source   ArtifactSource     `yaml:"source" json:"source"`
	Output   ArtifactFileOutput `yaml:"output" json:"output"`
	Checksum string             `yaml:"checksum,omitempty" json:"checksum,omitempty"`
	Mode     string             `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type ArtifactFileGroup struct {
	Group   string             `yaml:"group" json:"group"`
	Targets []ArtifactTarget   `yaml:"targets,omitempty" json:"targets,omitempty"`
	Items   []ArtifactFileItem `yaml:"items" json:"items"`
}

type ArtifactImageItem struct {
	Image string `yaml:"image" json:"image"`
}

type ArtifactImageGroup struct {
	Group   string              `yaml:"group" json:"group"`
	Targets []ArtifactTarget    `yaml:"targets,omitempty" json:"targets,omitempty"`
	Items   []ArtifactImageItem `yaml:"items" json:"items"`
	Backend map[string]any      `yaml:"backend,omitempty" json:"backend,omitempty"`
	Output  map[string]any      `yaml:"output,omitempty" json:"output,omitempty"`
}

type ArtifactPackageItem struct {
	Name string `yaml:"name" json:"name"`
}

type ArtifactPackageGroup struct {
	Group   string                `yaml:"group" json:"group"`
	Targets []ArtifactTarget      `yaml:"targets" json:"targets"`
	Items   []ArtifactPackageItem `yaml:"items" json:"items"`
	Repo    map[string]any        `yaml:"repo,omitempty" json:"repo,omitempty"`
	Backend map[string]any        `yaml:"backend,omitempty" json:"backend,omitempty"`
}

type Phase struct {
	Name    string        `yaml:"name" json:"name"`
	Imports []PhaseImport `yaml:"imports,omitempty" json:"imports,omitempty"`
	Steps   []Step        `yaml:"steps,omitempty" json:"steps,omitempty"`
}

type PhaseImport struct {
	Path string `yaml:"path" json:"path"`
	When string `yaml:"when,omitempty" json:"when,omitempty"`
}

type Step struct {
	ID         string            `yaml:"id" json:"id"`
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   map[string]any    `yaml:"metadata" json:"metadata,omitempty"`
	When       string            `yaml:"when" json:"when,omitempty"`
	Register   map[string]string `yaml:"register" json:"register,omitempty"`
	Retry      int               `yaml:"retry" json:"retry,omitempty"`
	Timeout    string            `yaml:"timeout" json:"timeout,omitempty"`
	Spec       map[string]any    `yaml:"spec" json:"spec"`
}
