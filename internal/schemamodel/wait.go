package schemamodel

type WaitForServiceStepDocument struct {
	ID         string                 `json:"id"`
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"`
	Metadata   map[string]any         `json:"metadata,omitempty"`
	When       string                 `json:"when,omitempty"`
	Register   map[string]string      `json:"register,omitempty"`
	Retry      int                    `json:"retry,omitempty"`
	Timeout    string                 `json:"timeout,omitempty"`
	Spec       WaitForServiceStepSpec `json:"spec"`
}

type WaitForServiceStepSpec struct {
	Interval     string `json:"interval,omitempty"`
	InitialDelay string `json:"initialDelay,omitempty"`
	Name         string `json:"name,omitempty"`
	Timeout      string `json:"timeout,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
}

type WaitForCommandStepSpec struct {
	Interval     string   `json:"interval,omitempty"`
	InitialDelay string   `json:"initialDelay,omitempty"`
	Command      []string `json:"command,omitempty"`
	Timeout      string   `json:"timeout,omitempty"`
	PollInterval string   `json:"pollInterval,omitempty"`
}

type WaitForFileStepSpec struct {
	Interval     string `json:"interval,omitempty"`
	InitialDelay string `json:"initialDelay,omitempty"`
	Path         string `json:"path,omitempty"`
	Type         string `json:"type,omitempty"`
	NonEmpty     bool   `json:"nonEmpty,omitempty"`
	Timeout      string `json:"timeout,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
}

type WaitForMissingFileStepSpec struct {
	Interval     string `json:"interval,omitempty"`
	InitialDelay string `json:"initialDelay,omitempty"`
	Path         string `json:"path,omitempty"`
	Type         string `json:"type,omitempty"`
	Timeout      string `json:"timeout,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
}

type WaitTCPPortStepSpec struct {
	Interval     string `json:"interval,omitempty"`
	InitialDelay string `json:"initialDelay,omitempty"`
	Address      string `json:"address,omitempty"`
	Port         string `json:"port,omitempty"`
	Timeout      string `json:"timeout,omitempty"`
	PollInterval string `json:"pollInterval,omitempty"`
}
