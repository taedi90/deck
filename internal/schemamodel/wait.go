package schemamodel

type WaitStepDocument struct {
	ID         string            `json:"id"`
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	When       string            `json:"when,omitempty"`
	Register   map[string]string `json:"register,omitempty"`
	Retry      int               `json:"retry,omitempty"`
	Timeout    string            `json:"timeout,omitempty"`
	Spec       WaitStepSpec      `json:"spec"`
}

type WaitStepSpec struct {
	Action       string   `json:"action"`
	Interval     string   `json:"interval,omitempty"`
	InitialDelay string   `json:"initialDelay,omitempty"`
	Name         string   `json:"name,omitempty"`
	Command      []string `json:"command,omitempty"`
	Path         string   `json:"path,omitempty"`
	Type         string   `json:"type,omitempty"`
	NonEmpty     bool     `json:"nonEmpty,omitempty"`
	Address      string   `json:"address,omitempty"`
	Port         string   `json:"port,omitempty"`
	Timeout      string   `json:"timeout,omitempty"`
	PollInterval string   `json:"pollInterval,omitempty"`
	State        string   `json:"state,omitempty"`
}
