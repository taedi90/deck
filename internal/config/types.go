package config

type Workflow struct {
	Role           string         `yaml:"role" json:"-"`
	Version        string         `yaml:"version" json:"version"`
	Vars           map[string]any `yaml:"vars" json:"vars,omitempty"`
	Phases         []Phase        `yaml:"phases,omitempty" json:"phases,omitempty"`
	Steps          []Step         `yaml:"steps,omitempty" json:"-"`
	StateKey       string         `yaml:"-" json:"-"`
	WorkflowSHA256 string         `yaml:"-" json:"-"`
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
