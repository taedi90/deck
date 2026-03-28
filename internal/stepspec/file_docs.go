package stepspec

var (
	commonFileFieldDocs = map[string]FieldDoc{
		"spec.path":                 {Description: "Destination path on the node.", Example: "/etc/containerd/config.toml"},
		"spec.mode":                 {Description: "File permissions in octal notation applied after the step completes.", Example: "0644"},
		"spec.source":               {Description: "Structured source descriptor for copy or extraction operations.", Example: "{path:/etc/kubernetes/admin.conf}"},
		"spec.source.url":           {Description: "URL to fetch the file from during prepare.", Example: "https://mirror.example.com/runc"},
		"spec.source.path":          {Description: "Local filesystem path to use as the source.", Example: "/opt/cache/runc"},
		"spec.source.sha256":        {Description: "Expected SHA-256 checksum for the fetched file.", Example: "abc123..."},
		"spec.source.bundle":        {Description: "Reference to a file already stored in the prepared bundle.", Example: "{root:files,path:bin/linux/amd64/runc}"},
		"spec.source.bundle.root":   {Description: "Bundle root category to read from.", Example: "files"},
		"spec.source.bundle.path":   {Description: "Relative path within the selected bundle root.", Example: "bin/linux/amd64/runc"},
		"spec.fetch":                {Description: "Optional download transport settings used when the source may need to be fetched.", Example: "{offlineOnly:true}"},
		"spec.fetch.offlineOnly":    {Description: "Restrict fetches to offline-safe sources only.", Example: "true"},
		"spec.fetch.sources":        {Description: "Ordered list of fetch source candidates tried for the transfer.", Example: "[{type:url,url:https://mirror.example.com/runc}]"},
		"spec.fetch.sources[].type": {Description: "Source selector type for a fetch candidate.", Example: "url"},
		"spec.fetch.sources[].path": {Description: "Local path source used when `type` is `path`.", Example: "/opt/cache/runc"},
		"spec.fetch.sources[].url":  {Description: "Remote URL source used when `type` is `url`.", Example: "https://mirror.example.com/runc"},
	}

	downloadFileFieldDocs = mergeFieldDocs(commonFileFieldDocs, map[string]FieldDoc{
		"spec.items":                      {Description: "Optional list form for batching multiple download items in one step.", Example: "[{source:{url:https://mirror.example.com/runc},outputPath:files/bin/runc,mode:0755}]"},
		"spec.source":                     {Description: "Structured source descriptor for the download.", Example: "{url:https://mirror.example.com/runc,sha256:abc123...}"},
		"spec.outputPath":                 {Description: "Bundle-relative output path for the downloaded artifact.", Example: "files/bin/runc"},
		"spec.timeout":                    {Description: "Optional per-transfer timeout for the download.", Example: "30s"},
		"spec.items[].source":             {Description: "Source descriptor for this item.", Example: "{url:https://mirror.example.com/runc}"},
		"spec.items[].source.url":         {Description: "URL to fetch for this item.", Example: "https://mirror.example.com/runc"},
		"spec.items[].source.path":        {Description: "Local path source for this item.", Example: "/opt/cache/runc"},
		"spec.items[].source.sha256":      {Description: "Expected SHA-256 checksum for this item.", Example: "abc123..."},
		"spec.items[].source.bundle":      {Description: "Bundle reference source for this item.", Example: "{root:files,path:bin/linux/amd64/runc}"},
		"spec.items[].source.bundle.root": {Description: "Bundle root category for this item.", Example: "files"},
		"spec.items[].source.bundle.path": {Description: "Relative path within the selected bundle root for this item.", Example: "bin/linux/amd64/runc"},
		"spec.items[].fetch":              {Description: "Optional transport policy for this item.", Example: "{offlineOnly:true}"},
		"spec.items[].fetch.offlineOnly":  {Description: "Restrict this item to offline-safe sources only.", Example: "true"},
		"spec.items[].fetch.sources":      {Description: "Ordered list of source candidates for this item.", Example: "[{type:url,url:https://mirror.example.com/runc}]"},
		"spec.items[].outputPath":         {Description: "Bundle-relative output path for this item.", Example: "files/bin/runc"},
		"spec.items[].mode":               {Description: "File permissions applied after this item is written.", Example: "0755"},
	})

	_ = registerToolDoc("DownloadFile", ToolDocMetadata{
		Example:   "kind: DownloadFile\nspec:\n  source:\n    url: https://mirror.example.com/runc\n    sha256: abc123...\n  outputPath: files/bin/runc\n  mode: \"0755\"\n",
		FieldDocs: downloadFileFieldDocs,
		Notes: []string{
			"`DownloadFile` writes into prepared bundle storage through `outputPath` rather than a node path.",
			"Omit `outputPath` unless later steps need a stable custom prepared location.",
			"Use `template` instead of `content` only for `WriteFile`; `DownloadFile` always uses explicit sources.",
		},
	})

	_ = registerToolDoc("WriteFile", ToolDocMetadata{
		Example: "kind: WriteFile\nspec:\n  path: /etc/containerd/config.toml\n  template: |\n    [plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n      SystemdCgroup = {{ .vars.systemdCgroup }}\n  mode: \"0644\"\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":     {Description: "Destination path on the node.", Example: "/etc/containerd/config.toml"},
			"spec.content":  {Description: "Inline file content written verbatim to `path`.", Example: "[offline-base]\nbaseurl=http://repo.local"},
			"spec.template": {Description: "Inline multi-line content rendered with current vars before writing.", Example: "[Service]\nEnvironment=ROLE={{ .vars.role }}"},
			"spec.mode":     {Description: "File permissions in octal notation applied after the write completes.", Example: "0644"},
		},
		Notes: []string{"Use `template` instead of `content` when the body needs variable interpolation."},
	})

	_ = registerToolDoc("CopyFile", ToolDocMetadata{
		Example: "kind: CopyFile\nspec:\n  source:\n    path: /etc/kubernetes/admin.conf\n  path: /home/vagrant/.kube/config\n  mode: \"0644\"\n",
		FieldDocs: map[string]FieldDoc{
			"spec.source":               commonFileFieldDocs["spec.source"],
			"spec.source.url":           commonFileFieldDocs["spec.source.url"],
			"spec.source.path":          commonFileFieldDocs["spec.source.path"],
			"spec.source.sha256":        commonFileFieldDocs["spec.source.sha256"],
			"spec.source.bundle":        commonFileFieldDocs["spec.source.bundle"],
			"spec.source.bundle.root":   commonFileFieldDocs["spec.source.bundle.root"],
			"spec.source.bundle.path":   commonFileFieldDocs["spec.source.bundle.path"],
			"spec.fetch":                commonFileFieldDocs["spec.fetch"],
			"spec.fetch.offlineOnly":    commonFileFieldDocs["spec.fetch.offlineOnly"],
			"spec.fetch.sources":        commonFileFieldDocs["spec.fetch.sources"],
			"spec.fetch.sources[].type": commonFileFieldDocs["spec.fetch.sources[].type"],
			"spec.fetch.sources[].path": commonFileFieldDocs["spec.fetch.sources[].path"],
			"spec.fetch.sources[].url":  commonFileFieldDocs["spec.fetch.sources[].url"],
			"spec.path":                 {Description: "Destination path on the node.", Example: "/home/vagrant/.kube/config"},
			"spec.mode":                 {Description: "File permissions in octal notation applied after the copy completes.", Example: "0644"},
		},
		Notes: []string{"Use `source.path` for simple local paths and `source.bundle` or `source.url` when the source is structured or external."},
	})

	_ = registerToolDoc("EditFile", ToolDocMetadata{
		Example: "kind: EditFile\nspec:\n  path: /etc/containerd/config.toml\n  edits:\n    - match: SystemdCgroup = false\n      replaceWith: SystemdCgroup = true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":                {Description: "File path to edit in place.", Example: "/etc/containerd/config.toml"},
			"spec.backup":              {Description: "Create a `.bak` copy of the original file before overwriting it.", Example: "true"},
			"spec.edits":               {Description: "Ordered list of match/replace rules applied sequentially to the file.", Example: "[{match:SystemdCgroup = false,replaceWith:SystemdCgroup = true}]"},
			"spec.edits[].match":       {Description: "Literal string or pattern to search for in the file.", Example: "SystemdCgroup = false"},
			"spec.edits[].replaceWith": {Description: "Replacement string applied where `match` is found.", Example: "SystemdCgroup = true"},
			"spec.edits[].op":          {Description: "Edit operation type. Defaults to `replace`.", Example: "replace"},
			"spec.mode":                {Description: "File permissions in octal notation applied after the edit completes.", Example: "0644"},
		},
		Notes: []string{"Use `EditTOML`, `EditYAML`, or `EditJSON` when structured edits are available and less brittle."},
	})

	_ = registerToolDoc("EditTOML", ToolDocMetadata{
		Example: "kind: EditTOML\nspec:\n  path: /etc/containerd/config.toml\n  edits:\n    - op: set\n      rawPath: plugins.\"io.containerd.grpc.v1.cri\".registry.config_path\n      value: /etc/containerd/certs.d\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":            {Description: "TOML file path to edit in place.", Example: "/etc/containerd/config.toml"},
			"spec.createIfMissing": {Description: "Create a new empty TOML document when the file does not exist.", Example: "true"},
			"spec.edits":           {Description: "Ordered list of structured edits applied sequentially to the TOML document.", Example: "[{op:set,rawPath:plugins.\"io.containerd.grpc.v1.cri\".registry.config_path,value:/etc/containerd/certs.d}]"},
			"spec.edits[].op":      {Description: "Structured edit operation.", Example: "set"},
			"spec.edits[].rawPath": {Description: "Quoted-segment TOML path.", Example: "plugins.\"io.containerd.grpc.v1.cri\".registry.config_path"},
			"spec.edits[].value":   {Description: "Typed edit value.", Example: "/etc/containerd/certs.d"},
			"spec.mode":            {Description: "Optional file permissions to apply after the edit completes.", Example: "0644"},
		},
		Notes: []string{"Use this for generic TOML editing when a domain-specific step is unnecessary."},
	})

	_ = registerToolDoc("EditYAML", ToolDocMetadata{
		Example: "kind: EditYAML\nspec:\n  path: /etc/kubernetes/kubeadm-config.yaml\n  edits:\n    - op: set\n      rawPath: ClusterConfiguration.imageRepository\n      value: registry.local/k8s\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":            {Description: "YAML file path to edit in place.", Example: "/etc/kubernetes/kubeadm-config.yaml"},
			"spec.createIfMissing": {Description: "Create a new empty YAML document when the file does not exist.", Example: "true"},
			"spec.edits":           {Description: "Ordered list of structured edits applied sequentially to the YAML document.", Example: "[{op:set,rawPath:spec.template.spec.containers.0.image,value:registry.local/app:v1}]"},
			"spec.edits[].op":      {Description: "Structured edit operation.", Example: "set"},
			"spec.edits[].rawPath": {Description: "Dot-path with optional quoted key segments and numeric array indexes.", Example: "spec.template.spec.containers.0.image"},
			"spec.edits[].value":   {Description: "Typed edit value.", Example: "registry.local/k8s"},
			"spec.mode":            {Description: "Optional file permissions to apply after the edit completes.", Example: "0644"},
		},
		Notes: []string{"Comment placement, anchors, aliases, merge keys, and style preservation are not guaranteed."},
	})

	_ = registerToolDoc("EditJSON", ToolDocMetadata{
		Example: "kind: EditJSON\nspec:\n  path: /etc/cni/net.d/10-custom.conflist\n  edits:\n    - op: set\n      rawPath: plugins.0.type\n      value: bridge\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":            {Description: "JSON file path to edit in place.", Example: "/etc/cni/net.d/10-custom.conflist"},
			"spec.createIfMissing": {Description: "Create a new empty JSON object when the file does not exist.", Example: "true"},
			"spec.edits":           {Description: "Ordered list of structured edits applied sequentially to the JSON document.", Example: "[{op:set,rawPath:plugins.0.type,value:bridge}]"},
			"spec.edits[].op":      {Description: "Structured edit operation.", Example: "set"},
			"spec.edits[].rawPath": {Description: "Dot-path with optional quoted key segments and numeric array indexes.", Example: "plugins.0.type"},
			"spec.edits[].value":   {Description: "Typed edit value.", Example: "bridge"},
			"spec.mode":            {Description: "Optional file permissions to apply after the edit completes.", Example: "0644"},
		},
	})

	_ = registerToolDoc("ExtractArchive", ToolDocMetadata{
		Example: "kind: ExtractArchive\nspec:\n  source:\n    path: /tmp/cni-plugins.tgz\n  path: /opt/cni/bin\n  include: [bridge, loopback]\n",
		FieldDocs: map[string]FieldDoc{
			"spec.source":               {Description: "Structured source descriptor for the archive to extract.", Example: "{path:/tmp/cni-plugins.tgz}"},
			"spec.source.url":           commonFileFieldDocs["spec.source.url"],
			"spec.source.path":          commonFileFieldDocs["spec.source.path"],
			"spec.source.sha256":        commonFileFieldDocs["spec.source.sha256"],
			"spec.source.bundle":        commonFileFieldDocs["spec.source.bundle"],
			"spec.source.bundle.root":   commonFileFieldDocs["spec.source.bundle.root"],
			"spec.source.bundle.path":   commonFileFieldDocs["spec.source.bundle.path"],
			"spec.fetch":                commonFileFieldDocs["spec.fetch"],
			"spec.fetch.offlineOnly":    commonFileFieldDocs["spec.fetch.offlineOnly"],
			"spec.fetch.sources":        commonFileFieldDocs["spec.fetch.sources"],
			"spec.fetch.sources[].type": commonFileFieldDocs["spec.fetch.sources[].type"],
			"spec.fetch.sources[].path": commonFileFieldDocs["spec.fetch.sources[].path"],
			"spec.fetch.sources[].url":  commonFileFieldDocs["spec.fetch.sources[].url"],
			"spec.path":                 {Description: "Destination directory on the node.", Example: "/opt/cni/bin"},
			"spec.include":              {Description: "Optional archive members to extract. Extract all members when omitted.", Example: "[bridge,loopback]"},
			"spec.mode":                 {Description: "File permissions applied to extracted files when supported.", Example: "0755"},
		},
		Notes: []string{"Extract all archive members when `include` is omitted."},
	})
)

func mergeFieldDocs(base map[string]FieldDoc, extra map[string]FieldDoc) map[string]FieldDoc {
	out := make(map[string]FieldDoc, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
