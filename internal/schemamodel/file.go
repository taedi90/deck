package schemamodel

type FileStepDocument struct {
	ID         string            `json:"id"`
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	When       string            `json:"when,omitempty"`
	Register   map[string]string `json:"register,omitempty"`
	Retry      int               `json:"retry,omitempty"`
	Timeout    string            `json:"timeout,omitempty"`
	Spec       FileStepSpec      `json:"spec"`
}

type FileStepSpec struct {
	Action              string            `json:"action,omitempty"`
	Path                string            `json:"path,omitempty"`
	Content             string            `json:"content,omitempty"`
	ContentFromTemplate string            `json:"contentFromTemplate,omitempty"`
	Mode                string            `json:"mode,omitempty"`
	Src                 string            `json:"src,omitempty"`
	Dest                string            `json:"dest,omitempty"`
	Backup              bool              `json:"backup,omitempty"`
	Edits               []FileEditRule    `json:"edits,omitempty"`
	Owner               string            `json:"owner,omitempty"`
	Group               string            `json:"group,omitempty"`
	Source              *FileSource       `json:"source,omitempty"`
	Fetch               map[string]any    `json:"fetch,omitempty"`
	Output              *FileOutputTarget `json:"output,omitempty"`
}

type FileEditRule struct {
	Match string `json:"match"`
	With  string `json:"with,omitempty"`
	Op    string `json:"op,omitempty"`
}

type FileSource struct {
	URL    string         `json:"url,omitempty"`
	Path   string         `json:"path,omitempty"`
	SHA256 string         `json:"sha256,omitempty"`
	Bundle *FileBundleRef `json:"bundle,omitempty"`
}

type FileBundleRef struct {
	Root string `json:"root"`
	Path string `json:"path"`
}

type FileOutputTarget struct {
	Path  string `json:"path"`
	Chmod string `json:"chmod,omitempty"`
}
