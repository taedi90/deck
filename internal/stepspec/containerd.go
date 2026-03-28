package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

// Write the containerd config file on the node.
// @deck.when Use this when the node runtime needs a managed containerd config.toml.
// @deck.note Use `WriteContainerdConfig` for the main `config.toml` only.
// @deck.note Use `WriteContainerdRegistryHosts` separately when mirrors or trust policy need per-registry `hosts.toml` files.
// @deck.example
// kind: WriteContainerdConfig
// spec:
//
//	path: /etc/containerd/config.toml
//	createDefault: true
//	versionPolicy: preserve
//	rawSettings:
//	  - op: set
//	    rawPath: plugins."io.containerd.grpc.v1.cri".registry.config_path
//	    value: /etc/containerd/certs.d
type WriteContainerdConfig struct {
	// Destination path for the generated `config.toml`.
	// @deck.example /etc/containerd/config.toml
	Path string `json:"path"`
	// Generate a base config with `containerd config default` when the file does not exist.
	// @deck.example true
	CreateDefault *bool `json:"createDefault"`
	// How the step chooses or enforces the containerd config version.
	// @deck.example require-v3
	VersionPolicy string `json:"versionPolicy"`
	// Ordered containerd config edits.
	// @deck.example [{op:set,rawPath:plugins."io.containerd.grpc.v1.cri".registry.config_path,value:/etc/containerd/certs.d}]
	RawSettings []ContainerdConfigSetting `json:"rawSettings"`
	// Maximum total duration for config rendering and writes.
	// @deck.example 5m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[WriteContainerdConfig](stepmeta.Definition{Kind: "WriteContainerdConfig", Family: "containerd", FamilyTitle: "Containerd", DocsPage: "containerd", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "containerd.config.schema.json"})

type ContainerdConfigSetting struct {
	// Edit operation.
	// @deck.example set
	Op string `json:"op"`
	// Optional logical containerd config key.
	// @deck.example runtime.runtimes.runc.options.SystemdCgroup
	Key string `json:"key"`
	// Optional direct TOML path for advanced use cases.
	// @deck.example plugins."io.containerd.grpc.v1.cri".registry.config_path
	RawPath string `json:"rawPath"`
	// Typed value for the edit.
	// @deck.example true
	Value any `json:"value,omitempty"`
}

type ContainerdRegistryHost struct {
	// Registry namespace whose `hosts.toml` file will be written.
	// @deck.example registry.k8s.io
	Registry string `json:"registry"`
	// Upstream registry server URL recorded in the generated `hosts.toml` file.
	// @deck.example https://registry.k8s.io
	Server string `json:"server"`
	// Mirror host URL that containerd should contact.
	// @deck.example http://registry.local:5000
	Host string `json:"host"`
	// Capabilities granted to the mirror host.
	// @deck.example [pull,resolve]
	Capabilities []string `json:"capabilities"`
	// Skip TLS verification for the mirror host.
	// @deck.example true
	SkipVerify bool `json:"skipVerify"`
}

// Write containerd registry host configuration for mirrors and trust policy.
// @deck.when Use this when containerd should resolve pulls through explicit registry host configuration.
// @deck.note Use this step when the runtime should resolve pulls through an internal mirror or custom trust policy.
// @deck.example
// kind: WriteContainerdRegistryHosts
// spec:
//
//	path: /etc/containerd/certs.d
//	registryHosts:
//	  - registry: registry.k8s.io
//	    server: https://registry.k8s.io
//	    host: http://registry.local:5000
//	    capabilities: [pull, resolve]
//	    skipVerify: true
type WriteContainerdRegistryHosts struct {
	// Directory where per-registry `hosts.toml` files are written.
	// @deck.example /etc/containerd/certs.d
	Path string `json:"path"`
	// Per-registry host entries written as `hosts.toml` files under `path`.
	// @deck.example [{registry:registry.k8s.io,host:http://mirror.local:5000}]
	RegistryHosts []ContainerdRegistryHost `json:"registryHosts"`
}

var _ = stepmeta.MustRegister[WriteContainerdRegistryHosts](stepmeta.Definition{Kind: "WriteContainerdRegistryHosts", Family: "containerd", FamilyTitle: "Containerd", DocsPage: "containerd", DocsOrder: 20, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "containerd.registry-hosts.schema.json"})
