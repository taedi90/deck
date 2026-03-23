package stepspec

type StructuredEdit struct {
	Op      string `json:"op"`
	RawPath string `json:"rawPath"`
	Value   any    `json:"value,omitempty"`
}

type EditTOML struct {
	Path            string           `json:"path"`
	CreateIfMissing *bool            `json:"createIfMissing"`
	Edits           []StructuredEdit `json:"edits"`
	Mode            string           `json:"mode"`
}

type EditYAML struct {
	Path            string           `json:"path"`
	CreateIfMissing *bool            `json:"createIfMissing"`
	Edits           []StructuredEdit `json:"edits"`
	Mode            string           `json:"mode"`
}

type EditJSON struct {
	Path            string           `json:"path"`
	CreateIfMissing *bool            `json:"createIfMissing"`
	Edits           []StructuredEdit `json:"edits"`
	Mode            string           `json:"mode"`
}
