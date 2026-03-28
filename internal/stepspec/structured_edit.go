package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

type StructuredEdit struct {
	// Structured edit operation.
	// @deck.example set
	Op string `json:"op"`
	// Path expression within the target document.
	// @deck.example plugins."io.containerd.grpc.v1.cri".registry.config_path
	RawPath string `json:"rawPath"`
	// Typed edit value.
	// @deck.example /etc/containerd/certs.d
	Value any `json:"value,omitempty"`
}

// Edit a TOML file in place using structured path operations.
// @deck.when Use this when TOML configuration should be updated without brittle string replacement.
// @deck.note Use this for generic TOML editing when a domain-specific step is unnecessary.
// @deck.example
// kind: EditTOML
// spec:
//
//	path: /etc/containerd/config.toml
//	edits:
//	  - op: set
//	    rawPath: plugins."io.containerd.grpc.v1.cri".registry.config_path
//	    value: /etc/containerd/certs.d
type EditTOML struct {
	// TOML file path to edit in place.
	// @deck.example /etc/containerd/config.toml
	Path string `json:"path"`
	// Create a new empty TOML document when the file does not exist.
	// @deck.example true
	CreateIfMissing *bool `json:"createIfMissing"`
	// Ordered list of structured edits applied sequentially.
	// @deck.example [{op:set,rawPath:plugins."io.containerd.grpc.v1.cri".registry.config_path,value:/etc/containerd/certs.d}]
	Edits []StructuredEdit `json:"edits"`
	// Optional file permissions to apply after the edit completes.
	// @deck.example 0644
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[EditTOML](stepmeta.Definition{Kind: "EditTOML", Family: "file", FamilyTitle: "File", DocsPage: "file", DocsOrder: 50, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "file.edit-toml.schema.json"})

// Edit a YAML file in place using structured path operations.
// @deck.when Use this for common map/list YAML updates where direct text replacement is too fragile.
// @deck.note Comment placement, anchors, aliases, merge keys, and style preservation are not guaranteed.
// @deck.example
// kind: EditYAML
// spec:
//
//	path: /etc/kubernetes/kubeadm-config.yaml
//	edits:
//	  - op: set
//	    rawPath: ClusterConfiguration.imageRepository
//	    value: registry.local/k8s
type EditYAML struct {
	// YAML file path to edit in place.
	// @deck.example /etc/kubernetes/kubeadm-config.yaml
	Path string `json:"path"`
	// Create a new empty YAML document when the file does not exist.
	// @deck.example true
	CreateIfMissing *bool `json:"createIfMissing"`
	// Ordered list of structured edits applied sequentially.
	// @deck.example [{op:set,rawPath:spec.template.spec.containers.0.image,value:registry.local/app:v1}]
	Edits []StructuredEdit `json:"edits"`
	// Optional file permissions to apply after the edit completes.
	// @deck.example 0644
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[EditYAML](stepmeta.Definition{Kind: "EditYAML", Family: "file", FamilyTitle: "File", DocsPage: "file", DocsOrder: 60, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "file.edit-yaml.schema.json", Ask: stepmeta.AskMetadata{MatchSignals: []string{"yaml", "edit", "patch", "config"}}})

// Edit a JSON file in place using structured path operations.
// @deck.when Use this when JSON configuration should be modified by path instead of full rewrites.
// @deck.example
// kind: EditJSON
// spec:
//
//	path: /etc/cni/net.d/10-custom.conflist
//	edits:
//	  - op: set
//	    rawPath: plugins.0.type
//	    value: bridge
type EditJSON struct {
	// JSON file path to edit in place.
	// @deck.example /etc/cni/net.d/10-custom.conflist
	Path string `json:"path"`
	// Create a new empty JSON object when the file does not exist.
	// @deck.example true
	CreateIfMissing *bool `json:"createIfMissing"`
	// Ordered list of structured edits applied sequentially.
	// @deck.example [{op:set,rawPath:plugins.0.type,value:bridge}]
	Edits []StructuredEdit `json:"edits"`
	// Optional file permissions to apply after the edit completes.
	// @deck.example 0644
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[EditJSON](stepmeta.Definition{Kind: "EditJSON", Family: "file", FamilyTitle: "File", DocsPage: "file", DocsOrder: 70, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "file.edit-json.schema.json"})
