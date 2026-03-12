package prepare

type manifestFile struct {
	Entries []manifestEntry `json:"entries"`
}

type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
