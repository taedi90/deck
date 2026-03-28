package stepspec

var (
	_ = registerToolDoc("DownloadImage", ToolDocMetadata{
		Example: "kind: DownloadImage\nspec:\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n    - registry.example.com/platform/pause:3.9\n  auth:\n    - registry: registry.example.com\n      basic:\n        username: \"{{ .vars.registryUser }}\"\n        password: \"{{ .vars.registryPassword }}\"\n",
		FieldDocs: map[string]FieldDoc{
			"spec.images":                {Description: "Fully qualified image references to download.", Example: "[registry.k8s.io/pause:3.9]"},
			"spec.auth":                  {Description: "Optional registry authentication entries used during download.", Example: "[{registry:registry.example.com,basic:{username:robot,password:${REGISTRY_PASSWORD}}}]"},
			"spec.auth[].registry":       {Description: "Registry host matched against each image reference.", Example: "registry.example.com"},
			"spec.auth[].basic":          {Description: "Explicit basic-auth credentials used when downloading from the matched registry.", Example: "{username:robot,password:${REGISTRY_PASSWORD}}"},
			"spec.auth[].basic.username": {Description: "Registry username used for basic authentication.", Example: "robot"},
			"spec.auth[].basic.password": {Description: "Registry password or access token paired with `basic.username`.", Example: "${REGISTRY_PASSWORD}"},
			"spec.outputDir":             {Description: "Optional bundle-relative directory where per-image tar archives are written.", Example: "images/control-plane"},
			"spec.backend":               {Description: "Backend-specific download settings. Applies to `DownloadImage` only.", Example: "{engine:go-containerregistry}"},
			"spec.backend.engine":        {Description: "Image download engine.", Example: "go-containerregistry"},
		},
		Notes: []string{
			"Use `DownloadImage` during prepare to collect required images for offline use.",
			"Omit `outputDir` unless you need a custom bundle subdirectory; deck writes to `images/` by default.",
			"`spec.auth` is optional and only applies to `DownloadImage`.",
		},
	})
	_ = registerToolDoc("LoadImage", ToolDocMetadata{
		Example: "kind: LoadImage\nspec:\n  sourceDir: images/control-plane\n  runtime: ctr\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n",
		FieldDocs: map[string]FieldDoc{
			"spec.command":   {Description: "Optional runtime command override. This command may include `{archive}` placeholders that deck substitutes per image archive.", Example: "[ctr,-n,k8s.io,images,import,{archive}]"},
			"spec.images":    {Description: "Fully qualified image references to load.", Example: "[registry.k8s.io/pause:3.9]"},
			"spec.runtime":   {Description: "Runtime loader used by `LoadImage`.", Example: "ctr"},
			"spec.sourceDir": {Description: "Directory containing prepared image archives to load into the runtime.", Example: "images/control-plane"},
		},
		Notes: []string{"Use `LoadImage` during apply when archives must be imported into the runtime."},
	})
	_ = registerToolDoc("VerifyImage", ToolDocMetadata{
		Example: "kind: VerifyImage\nspec:\n  command: [ctr, -n, k8s.io, images, list, -q]\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n",
		FieldDocs: map[string]FieldDoc{
			"spec.command": {Description: "Optional image-listing command override.", Example: "[ctr,-n,k8s.io,images,list,-q]"},
			"spec.images":  {Description: "Fully qualified image references that must already exist on the node.", Example: "[registry.k8s.io/pause:3.9]"},
		},
		Notes: []string{"Use `VerifyImage` when the runtime should already contain the required images and only verification is needed."},
	})
)
