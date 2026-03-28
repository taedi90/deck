package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

type ImageAuthBasic struct {
	// Registry username used for basic authentication.
	// @deck.example robot
	Username string `json:"username"`
	// Registry password or access token paired with `username`.
	// @deck.example ${REGISTRY_PASSWORD}
	Password string `json:"password"`
}

type ImageAuth struct {
	// Registry host matched against each image reference.
	// @deck.example registry.example.com
	Registry string `json:"registry"`
	// Explicit basic-auth credentials used for the matched registry.
	// @deck.example {username:robot,password:${REGISTRY_PASSWORD}}
	Basic ImageAuthBasic `json:"basic"`
}

type ImageBackend struct {
	// Image download engine implementation.
	// @deck.example go-containerregistry
	Engine string `json:"engine"`
}

// Download container images into prepared bundle storage.
// @deck.when Use this during prepare to collect required images for offline use.
// @deck.note Omit `outputDir` unless you need a dedicated image subdirectory; deck writes to `images/` by default.
// @deck.note `spec.auth` is optional and only applies to `DownloadImage`.
// @deck.example
// kind: DownloadImage
// spec:
//
//	images:
//	  - registry.k8s.io/kube-apiserver:v1.30.1
//	  - registry.example.com/platform/pause:3.9
//	auth:
//	  - registry: registry.example.com
//	    basic:
//	      username: "{{ .vars.registryUser }}"
//	      password: "{{ .vars.registryPassword }}"
type DownloadImage struct {
	// Fully qualified image references to download.
	// @deck.example [registry.k8s.io/pause:3.9]
	Images []string `json:"images"`
	// Optional registry authentication entries used during download.
	// @deck.example [{registry:registry.example.com,basic:{username:robot,password:${REGISTRY_PASSWORD}}}]
	Auth []ImageAuth `json:"auth"`
	// Backend-specific download settings.
	// @deck.example {engine:go-containerregistry}
	Backend ImageBackend `json:"backend"`
	// Optional bundle-relative directory for per-image tar archives.
	// @deck.example images/control-plane
	OutputDir string `json:"outputDir"`
	// Optional total timeout for the download step.
	// @deck.example 10m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[DownloadImage](stepmeta.Definition{
	Kind:        "DownloadImage",
	Family:      "image",
	FamilyTitle: "Image",
	DocsPage:    "image",
	DocsOrder:   10,
	Visibility:  "public",
	Roles:       []string{"prepare"},
	Outputs:     []string{"artifacts"},
	SchemaFile:  "image.download.schema.json",
	Ask: stepmeta.AskMetadata{
		MatchSignals:             []string{"air-gapped", "image", "images", "registry", "mirror", "offline", "prepare"},
		KeyFields:                []string{"spec.images", "spec.auth", "spec.backend", "spec.outputDir"},
		CommonMistakes:           []string{"Use DownloadImage during prepare for offline image collection instead of falling back to Command scripts.", "Keep spec.images as a real YAML array and spec.backend.engine as a literal allowed value."},
		RepairHints:              []string{"Use DownloadImage in prepare to collect image archives for offline apply instead of using Command for docker pull or docker save."},
		ValidationHints:          []stepmeta.ValidationHint{{ErrorContains: "spec.backend.engine must be one of", Fix: "Keep spec.backend.engine as the literal value `go-containerregistry`; do not replace it with a vars template."}, {ErrorContains: "is not supported for role prepare", Fix: "For prepare-time image collection, use DownloadImage instead of Command so the step matches the prepare role."}},
		ConstrainedLiteralFields: []stepmeta.ConstrainedLiteralField{{Path: "spec.backend.engine", AllowedValues: []string{"go-containerregistry"}, Guidance: "Keep spec.backend.engine as a literal enum, not a vars template."}},
	},
})

// Load prepared image archives into the local container runtime.
// @deck.when Use this during apply before verifying or using images from an offline bundle.
// @deck.note `command` may include `{archive}` placeholders that deck substitutes per image archive.
// @deck.example
// kind: LoadImage
// spec:
//
//	sourceDir: images/control-plane
//	runtime: ctr
//	images:
//	  - registry.k8s.io/kube-apiserver:v1.30.1
type LoadImage struct {
	// Image references to load from the prepared archives.
	// @deck.example [registry.k8s.io/kube-apiserver:v1.30.1]
	Images []string `json:"images"`
	// Directory containing prepared image archives.
	// @deck.example images/control-plane
	SourceDir string `json:"sourceDir"`
	// Runtime loader to use for imports.
	// @deck.example ctr
	Runtime string `json:"runtime"`
	// Optional runtime command override.
	// @deck.example [ctr,-n,k8s.io,images,import,{archive}]
	Command []string `json:"command"`
	// Optional total timeout for the load step.
	// @deck.example 10m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[LoadImage](stepmeta.Definition{
	Kind:        "LoadImage",
	Family:      "image",
	FamilyTitle: "Image",
	DocsPage:    "image",
	DocsOrder:   20,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "image.load.schema.json",
	Ask: stepmeta.AskMetadata{
		MatchSignals:             []string{"air-gapped", "image", "images", "archive", "containerd", "docker", "offline"},
		KeyFields:                []string{"spec.images", "spec.sourceDir", "spec.runtime", "spec.command"},
		CommonMistakes:           []string{"Keep spec.images in the schema-supported shape from the step example.", "Do not replace the whole images collection with a single quoted template scalar."},
		RepairHints:              []string{"Return a schema-valid LoadImage spec using the documented image archive shape from ask metadata."},
		ConstrainedLiteralFields: []stepmeta.ConstrainedLiteralField{{Path: "spec.runtime", AllowedValues: []string{"auto", "ctr", "docker", "podman"}, Guidance: "Keep spec.runtime as a literal enum, not a vars template."}},
	},
})

// Verify that required container images already exist on the node.
// @deck.when Use this during apply when images should already be present and only need verification.
// @deck.note Use this instead of `LoadImage` when the runtime is expected to be pre-populated.
// @deck.example
// kind: VerifyImage
// spec:
//
//	command: [ctr, -n, k8s.io, images, list, -q]
//	images:
//	  - registry.k8s.io/kube-apiserver:v1.30.1
type VerifyImage struct {
	// Image references that must already exist in the runtime.
	// @deck.example [registry.k8s.io/kube-apiserver:v1.30.1]
	Images []string `json:"images"`
	// Optional image-listing command override.
	// @deck.example [ctr,-n,k8s.io,images,list,-q]
	Command []string `json:"command"`
	// Optional total timeout for the verification step.
	// @deck.example 5m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[VerifyImage](stepmeta.Definition{
	Kind:        "VerifyImage",
	Family:      "image",
	FamilyTitle: "Image",
	DocsPage:    "image",
	DocsOrder:   30,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "image.verify.schema.json",
})
