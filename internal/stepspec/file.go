package stepspec

type FileBundleRef struct {
	Root string `json:"root"`
	Path string `json:"path"`
}

type FileSource struct {
	URL    string         `json:"url"`
	Path   string         `json:"path"`
	SHA256 string         `json:"sha256"`
	Bundle *FileBundleRef `json:"bundle"`
}

type FileFetchSource struct {
	Type string `json:"type"`
	Path string `json:"path"`
	URL  string `json:"url"`
}

type FileFetch struct {
	OfflineOnly bool              `json:"offlineOnly"`
	Sources     []FileFetchSource `json:"sources"`
}

type DownloadFile struct {
	Items      []DownloadFileItem `json:"items,omitempty"`
	Source     FileSource         `json:"source"`
	Fetch      FileFetch          `json:"fetch"`
	OutputPath string             `json:"outputPath"`
	Mode       string             `json:"mode"`
	Timeout    string             `json:"timeout"`
}

type DownloadFileItem struct {
	Source     FileSource `json:"source"`
	Fetch      FileFetch  `json:"fetch"`
	OutputPath string     `json:"outputPath"`
	Mode       string     `json:"mode"`
}

type WriteFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Template string `json:"template"`
	Mode     string `json:"mode"`
}

type CopyFile struct {
	Source FileSource `json:"source"`
	Fetch  FileFetch  `json:"fetch"`
	Path   string     `json:"path"`
	Mode   string     `json:"mode"`
}

type ExtractArchive struct {
	Source  FileSource `json:"source"`
	Fetch   FileFetch  `json:"fetch"`
	Path    string     `json:"path"`
	Include []string   `json:"include"`
	Mode    string     `json:"mode"`
}

type EditFileRule struct {
	Match       string `json:"match"`
	ReplaceWith string `json:"replaceWith"`
	Op          string `json:"op"`
}

type EditFile struct {
	Path   string         `json:"path"`
	Backup *bool          `json:"backup"`
	Edits  []EditFileRule `json:"edits"`
	Mode   string         `json:"mode"`
}
