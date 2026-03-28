package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

type FileBundleRef struct {
	// Bundle root category to read from (`files`, `images`, or `packages`).
	// @deck.example files
	Root string `json:"root"`
	// Relative path within the selected bundle root.
	// @deck.example bin/linux/amd64/runc
	Path string `json:"path"`
}

type FileSource struct {
	// URL to fetch the file from during prepare.
	// @deck.example https://mirror.example.com/runc
	URL string `json:"url"`
	// Local filesystem path used as the source.
	// @deck.example /opt/cache/runc
	Path string `json:"path"`
	// Expected SHA-256 checksum for the fetched or copied file.
	// @deck.example abc123...
	SHA256 string `json:"sha256"`
	// Reference to a file already present in the bundle.
	// @deck.example {root:files,path:bin/linux/amd64/runc}
	Bundle *FileBundleRef `json:"bundle"`
}

type FileFetchSource struct {
	// Source selector type for multi-source fetches.
	// @deck.example url
	Type string `json:"type"`
	// Local path source used when `type` is `path`.
	// @deck.example /opt/cache/runc
	Path string `json:"path"`
	// Remote URL source used when `type` is `url`.
	// @deck.example https://mirror.example.com/runc
	URL string `json:"url"`
}

type FileFetch struct {
	// Restrict fetches to offline-safe sources only.
	// @deck.example true
	OfflineOnly bool `json:"offlineOnly"`
	// Ordered list of source candidates tried for the fetch.
	// @deck.example [{type:url,url:https://mirror.example.com/runc}]
	Sources []FileFetchSource `json:"sources"`
}

// Download a file into prepared bundle storage.
// @deck.when Use this during prepare to stage files into the bundle.
// @deck.note `DownloadFile` writes into bundle storage through `outputPath` rather than a node path.
// @deck.note Omit `outputPath` unless later steps need a stable custom bundle location.
// @deck.example
// kind: DownloadFile
// spec:
//
//	source:
//	  url: https://mirror.example.com/runc
//	  sha256: abc123...
//	outputPath: files/bin/runc
//	mode: "0755"
type DownloadFile struct {
	// Optional list form for batching multiple download items in one step.
	// @deck.example [{source:{url:https://mirror.example.com/runc},outputPath:files/bin/runc,mode:0755}]
	Items []DownloadFileItem `json:"items,omitempty"`
	// Structured source descriptor for the download.
	// @deck.example {url:https://mirror.example.com/runc,sha256:abc123...}
	Source FileSource `json:"source"`
	// Optional transport policy for the download.
	// @deck.example {offlineOnly:true}
	Fetch FileFetch `json:"fetch"`
	// Bundle-relative output path for the downloaded artifact.
	// @deck.example files/bin/runc
	OutputPath string `json:"outputPath"`
	// File permissions in octal notation applied after the download completes.
	// @deck.example 0755
	Mode string `json:"mode"`
	// Optional per-transfer timeout for the download.
	// @deck.example 30s
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[DownloadFile](stepmeta.Definition{
	Kind:        "DownloadFile",
	Family:      "file",
	FamilyTitle: "File",
	DocsPage:    "file",
	DocsOrder:   10,
	Visibility:  "public",
	Roles:       []string{"prepare"},
	Outputs:     []string{"outputPath", "outputPaths", "artifacts"},
	SchemaFile:  "file.download.schema.json",
	Ask:         stepmeta.AskMetadata{KeyFields: []string{"spec.source", "spec.fetch", "spec.mode"}},
})

type DownloadFileItem struct {
	// Source descriptor for this item.
	// @deck.example {url:https://mirror.example.com/runc}
	Source FileSource `json:"source"`
	// Optional transport policy for this item.
	// @deck.example {offlineOnly:true}
	Fetch FileFetch `json:"fetch"`
	// Bundle-relative output path for this item.
	// @deck.example files/bin/runc
	OutputPath string `json:"outputPath"`
	// File permissions applied after this item is written.
	// @deck.example 0755
	Mode string `json:"mode"`
}

// Write inline or templated file content to a destination path.
// @deck.when Use this to create or fully replace a managed file on the node.
// @deck.note Use `template` instead of `content` when the body needs variable interpolation.
// @deck.example
// kind: WriteFile
// spec:
//
//	path: /etc/motd
//	content: hello
type WriteFile struct {
	// Destination path on the node.
	// @deck.example /etc/motd
	Path string `json:"path"`
	// Inline content written verbatim to `path`.
	// @deck.example hello
	Content string `json:"content"`
	// Inline multi-line content rendered with the current vars before writing.
	// @deck.example Hello {{ .vars.name }}
	Template string `json:"template"`
	// File permissions in octal notation applied after the write completes.
	// @deck.example 0644
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[WriteFile](stepmeta.Definition{
	Kind:        "WriteFile",
	Family:      "file",
	FamilyTitle: "File",
	DocsPage:    "file",
	DocsOrder:   20,
	Visibility:  "public",
	Roles:       []string{"apply"},
	Outputs:     []string{"path"},
	SchemaFile:  "file.write.schema.json",
	Ask:         stepmeta.AskMetadata{MatchSignals: []string{"write", "file", "config", "motd", "content"}, KeyFields: []string{"spec.path", "spec.content", "spec.template", "spec.mode"}},
})

// Copy a file from a declared source to a destination path.
// @deck.when Use this to place a prepared or local file at its final location on the node.
// @deck.note Use `source.path` for simple local paths and `source.bundle` or `source.url` when the source is structured or external.
// @deck.example
// kind: CopyFile
// spec:
//
//	source:
//	  path: /etc/kubernetes/admin.conf
//	path: /home/vagrant/.kube/config
//	mode: "0644"
type CopyFile struct {
	// Structured source descriptor for the file to copy.
	// @deck.example {path:/etc/kubernetes/admin.conf}
	Source FileSource `json:"source"`
	// Optional transport policy when the source must be fetched first.
	// @deck.example {offlineOnly:true}
	Fetch FileFetch `json:"fetch"`
	// Destination path on the node.
	// @deck.example /home/vagrant/.kube/config
	Path string `json:"path"`
	// File permissions in octal notation applied after the copy completes.
	// @deck.example 0644
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[CopyFile](stepmeta.Definition{
	Kind:        "CopyFile",
	Family:      "file",
	FamilyTitle: "File",
	DocsPage:    "file",
	DocsOrder:   30,
	Visibility:  "public",
	Roles:       []string{"apply"},
	Outputs:     []string{"path"},
	SchemaFile:  "file.copy.schema.json",
	Ask:         stepmeta.AskMetadata{KeyFields: []string{"spec.source", "spec.path", "spec.mode"}},
})

// Extract an archive from a declared source into a destination directory.
// @deck.when Use this when prepared tarballs or local archives should be expanded onto the node.
// @deck.note Extract all members when `include` is omitted.
// @deck.example
// kind: ExtractArchive
// spec:
//
//	source:
//	  path: /tmp/cni-plugins.tgz
//	path: /opt/cni/bin
//	include: [bridge, loopback]
type ExtractArchive struct {
	// Structured source descriptor for the archive to extract.
	// @deck.example {path:/tmp/cni-plugins.tgz}
	Source FileSource `json:"source"`
	// Optional transport policy when the archive must be fetched first.
	// @deck.example {offlineOnly:true}
	Fetch FileFetch `json:"fetch"`
	// Destination directory on the node.
	// @deck.example /opt/cni/bin
	Path string `json:"path"`
	// Optional archive members to extract.
	// @deck.example [bridge,loopback]
	Include []string `json:"include"`
	// File permissions in octal notation applied to extracted files when supported.
	// @deck.example 0755
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[ExtractArchive](stepmeta.Definition{
	Kind:        "ExtractArchive",
	Family:      "file",
	FamilyTitle: "File",
	DocsPage:    "file",
	DocsOrder:   80,
	Visibility:  "public",
	Roles:       []string{"apply"},
	Outputs:     []string{"path"},
	SchemaFile:  "file.extract-archive.schema.json",
})

type EditFileRule struct {
	// Literal string or pattern to search for in the file.
	// @deck.example SystemdCgroup = false
	Match string `json:"match"`
	// Replacement string applied where `match` is found.
	// @deck.example SystemdCgroup = true
	ReplaceWith string `json:"replaceWith"`
	// Edit operation type. Defaults to `replace`.
	// @deck.example replace
	Op string `json:"op"`
}

// Edit an existing file in place using ordered match rules.
// @deck.when Use this for small in-place configuration edits when full file ownership is unnecessary.
// @deck.note Use `EditTOML`, `EditYAML`, or `EditJSON` when structured edits are available and less brittle.
// @deck.example
// kind: EditFile
// spec:
//
//	path: /etc/containerd/config.toml
//	edits:
//	  - match: SystemdCgroup = false
//	    replaceWith: SystemdCgroup = true
type EditFile struct {
	// File path to edit in place.
	// @deck.example /etc/containerd/config.toml
	Path string `json:"path"`
	// Create a `.bak` copy before overwriting the original file.
	// @deck.example true
	Backup *bool `json:"backup"`
	// Ordered match/replace rules applied sequentially.
	// @deck.example [{match:SystemdCgroup = false,replaceWith:SystemdCgroup = true}]
	Edits []EditFileRule `json:"edits"`
	// File permissions in octal notation applied after the edit completes.
	// @deck.example 0644
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[EditFile](stepmeta.Definition{
	Kind:        "EditFile",
	Family:      "file",
	FamilyTitle: "File",
	DocsPage:    "file",
	DocsOrder:   40,
	Visibility:  "public",
	Roles:       []string{"apply"},
	Outputs:     []string{"path"},
	SchemaFile:  "file.edit.schema.json",
	Ask:         stepmeta.AskMetadata{KeyFields: []string{"spec.path", "spec.edits", "spec.backup", "spec.mode"}},
})
