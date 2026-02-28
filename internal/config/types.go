package config

type Workflow struct {
	Version string         `yaml:"version" json:"version"`
	Imports []string       `yaml:"imports" json:"imports,omitempty"`
	Vars    map[string]any `yaml:"vars" json:"vars,omitempty"`
	Context Context        `yaml:"context" json:"context,omitempty"`
	Phases  []Phase        `yaml:"phases" json:"phases"`
}

type Context struct {
	BundleRoot string `yaml:"bundleRoot" json:"bundleRoot,omitempty"`
	StateFile  string `yaml:"stateFile" json:"stateFile,omitempty"`
}

type Phase struct {
	Name  string `yaml:"name" json:"name"`
	Steps []Step `yaml:"steps" json:"steps"`
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
