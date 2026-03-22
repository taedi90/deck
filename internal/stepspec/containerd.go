package stepspec

type WriteContainerdConfig struct {
	Path          string `json:"path"`
	ConfigPath    string `json:"configPath"`
	SystemdCgroup *bool  `json:"systemdCgroup"`
	CreateDefault *bool  `json:"createDefault"`
	Timeout       string `json:"timeout"`
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
