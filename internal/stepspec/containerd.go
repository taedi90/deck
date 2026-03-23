package stepspec

type WriteContainerdConfig struct {
	Path          string                    `json:"path"`
	CreateDefault *bool                     `json:"createDefault"`
	VersionPolicy string                    `json:"versionPolicy"`
	RawSettings   []ContainerdConfigSetting `json:"rawSettings"`
	Timeout       string                    `json:"timeout"`
}

type ContainerdConfigSetting struct {
	Op      string `json:"op"`
	Key     string `json:"key"`
	RawPath string `json:"rawPath"`
	Value   any    `json:"value,omitempty"`
}

type ContainerdRegistryHost struct {
	Registry     string   `json:"registry"`
	Server       string   `json:"server"`
	Host         string   `json:"host"`
	Capabilities []string `json:"capabilities"`
	SkipVerify   bool     `json:"skipVerify"`
}

type WriteContainerdRegistryHosts struct {
	Path          string                   `json:"path"`
	RegistryHosts []ContainerdRegistryHost `json:"registryHosts"`
}
