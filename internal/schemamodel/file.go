package schemamodel

type DownloadFileStepDocument struct {
	ID         string               `json:"id"`
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   map[string]any       `json:"metadata,omitempty"`
	When       string               `json:"when,omitempty"`
	Register   map[string]string    `json:"register,omitempty"`
	Retry      int                  `json:"retry,omitempty"`
	Timeout    string               `json:"timeout,omitempty"`
	Spec       DownloadFileStepSpec `json:"spec"`
}

type DownloadFileStepSpec struct {
	Items      []DownloadFileItem `json:"items,omitempty"`
	Source     *FileSource        `json:"source,omitempty"`
	Fetch      *FileFetch         `json:"fetch,omitempty"`
	OutputPath string             `json:"outputPath,omitempty"`
	Mode       string             `json:"mode,omitempty"`
}

type DownloadFileItem struct {
	Source     *FileSource `json:"source,omitempty"`
	Fetch      *FileFetch  `json:"fetch,omitempty"`
	OutputPath string      `json:"outputPath,omitempty"`
	Mode       string      `json:"mode,omitempty"`
}

type WriteFileStepSpec struct {
	Path     string `json:"path,omitempty"`
	Content  string `json:"content,omitempty"`
	Template string `json:"template,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

type CopyFileStepSpec struct {
	Source *FileSource `json:"source,omitempty"`
	Path   string      `json:"path,omitempty"`
	Mode   string      `json:"mode,omitempty"`
}

type ExtractArchiveStepSpec struct {
	Source  *FileSource `json:"source,omitempty"`
	Path    string      `json:"path,omitempty"`
	Include []string    `json:"include,omitempty"`
	Mode    string      `json:"mode,omitempty"`
}

type EditFileStepSpec struct {
	Path   string         `json:"path,omitempty"`
	Backup bool           `json:"backup,omitempty"`
	Edits  []EditFileRule `json:"edits,omitempty"`
	Mode   string         `json:"mode,omitempty"`
}

type EditFileRule struct {
	Match       string `json:"match"`
	ReplaceWith string `json:"replaceWith,omitempty"`
	Op          string `json:"op,omitempty"`
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

type FileFetch struct {
	OfflineOnly bool              `json:"offlineOnly,omitempty"`
	Sources     []FileFetchSource `json:"sources,omitempty"`
}

type FileFetchSource struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
}
