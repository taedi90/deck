package schemadoc

import (
	"maps"
	"sort"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
)

type FieldDoc struct {
	Description string
	Example     string
}

type ToolMetadata struct {
	Kind           string
	Category       string
	Summary        string
	WhenToUse      string
	Example        string
	ActionNotes    map[string]string
	ActionExamples map[string]string
	FieldDocs      map[string]FieldDoc
	Notes          []string
}

type PageMetadata struct {
	Title     string
	Summary   string
	Example   string
	FieldDocs map[string]FieldDoc
	Notes     []string
}

// commonFieldDocs describes the structural and execution-control fields shared by every step document.
// These are merged into every tool's FieldDocs so non-action pages and any action-specific references
// to shared fields use the same wording.
var commonFieldDocs = map[string]FieldDoc{
	"apiVersion": {Description: "Optional step API version. When omitted, deck resolves it from the top-level workflow `version`. When set, it must be a supported deck step API version.", Example: "deck/v1alpha1"},
	"id":         {Description: "Unique identifier for the step within the workflow. Used in logs and plan output.", Example: "configure-containerd"},
	"kind":       {Description: "Concrete typed step kind. Together with the effective `apiVersion`, it determines which schema is applied to `spec`.", Example: "WriteFile"},
	"spec":       {Description: "Step-specific configuration payload. Shape depends on the chosen `kind`.", Example: "{...}"},
	"when":       {Description: workflowcontract.WhenDescription(), Example: workflowcontract.WhenExample()},
	"retry":      {Description: "Number of times to retry the step after a failure before marking it as failed.", Example: "3"},
	"timeout":    {Description: "Maximum duration allowed for the step before it is cancelled. Accepts Go duration strings.", Example: "5m"},
	"register":   {Description: workflowcontract.RegisterDescription(), Example: workflowcontract.RegisterExample()},
	"metadata":   {Description: "Optional free-form annotation map attached to the step for tooling or audit purposes.", Example: "{owner: platform-team}"},
}

var toolMetadata = map[string]ToolMetadata{
	"Command": {
		Example: "kind: Command\nspec:\n  command: [\"systemctl\", \"status\", \"containerd\"]\n  timeout: 30s\n",
		FieldDocs: map[string]FieldDoc{
			"spec.command": {Description: "Command vector to execute. The first element is the binary; remaining elements are arguments.", Example: "[systemctl,restart,containerd]"},
			"spec.env":     {Description: "Additional environment variables passed to the command process as key-value pairs.", Example: "{KUBECONFIG:/etc/kubernetes/admin.conf}"},
			"spec.sudo":    {Description: "Prepend `sudo` before the command vector. Defaults to `false`.", Example: "false"},
			"spec.timeout": {Description: "Maximum duration for the command before it is killed. Overrides the step-level `timeout`.", Example: "30s"},
		},
		Notes: []string{
			"Prefer a typed step kind over `Command` whenever one is available — typed steps are easier to lint, review, and evolve.",
			"Use `spec.timeout` to bound commands that may hang rather than relying on the outer step timeout.",
		},
	},

	"WriteContainerdConfig": {
		Example: "kind: WriteContainerdConfig\nspec:\n  path: /etc/containerd/config.toml\n  createDefault: true\n  versionPolicy: preserve\n  rawSettings:\n    - op: set\n      rawPath: plugins.\"io.containerd.grpc.v1.cri\".registry.config_path\n      value: /etc/containerd/certs.d\n    - op: set\n      key: runtime.runtimes.runc.options.SystemdCgroup\n      value: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":                  {Description: "Destination path for the generated `config.toml`. Defaults to `/etc/containerd/config.toml`.", Example: "/etc/containerd/config.toml"},
			"spec.createDefault":         {Description: "Generate a base config with `containerd config default` when the file does not exist. Defaults to `true`.", Example: "true"},
			"spec.versionPolicy":         {Description: "How the step chooses or enforces the containerd config version. Use `preserve` to keep the existing version, or `require-v1`, `require-v2`, `require-v3` to pin it.", Example: "require-v3"},
			"spec.rawSettings":           {Description: "Ordered containerd config edits. Prefer `rawPath` when you need direct TOML path control, or `key` when you want deck to map a version-independent logical key across containerd v1, v2, and v3.", Example: "[{op:set,rawPath:plugins.\"io.containerd.grpc.v1.cri\".registry.config_path,value:/etc/containerd/certs.d}]"},
			"spec.rawSettings[].op":      {Description: "Edit operation. Use `set` for scalar updates, `delete` to remove a key, `appendUnique` to extend a string list without duplicates, or `replaceList` to replace a string list entirely.", Example: "set"},
			"spec.rawSettings[].key":     {Description: "Optional logical containerd config key. Examples include `registry.configPath`, `image.snapshotter`, and `runtime.runtimes.runc.options.SystemdCgroup`.", Example: "runtime.runtimes.runc.options.SystemdCgroup"},
			"spec.rawSettings[].rawPath": {Description: "Optional direct TOML path for advanced use cases. Use quoted segments when a key contains dots, for example `plugins.\"io.containerd.grpc.v1.cri\".registry.config_path`.", Example: "plugins.\"io.containerd.grpc.v1.cri\".registry.config_path"},
			"spec.rawSettings[].value":   {Description: "Typed value for the edit. Supported value types are string, boolean, integer, object, and string list depending on the target path and operation.", Example: "true"},
		},
		Notes: []string{
			"Use `WriteContainerdConfig` for the main `config.toml` only.",
			"Prefer `rawSettings[].rawPath` when you need full control over the exact TOML location.",
			"Use `rawSettings[].key` when you want deck to map a version-independent logical key for containerd config versions 1, 2, and 3.",
			"Use `WriteContainerdRegistryHosts` separately when mirrors or trust policy need per-registry `hosts.toml` files.",
		},
	},

	"WriteContainerdRegistryHosts": {
		Example: "kind: WriteContainerdRegistryHosts\nspec:\n  path: /etc/containerd/certs.d\n  registryHosts:\n    - registry: registry.k8s.io\n      server: https://registry.k8s.io\n      host: http://registry.local:5000\n      capabilities: [pull, resolve]\n      skipVerify: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":                         {Description: "Directory where per-registry `hosts.toml` files are written.", Example: "/etc/containerd/certs.d"},
			"spec.registryHosts":                {Description: "Per-registry host entries written as `hosts.toml` files under `path`. Each entry redirects a registry to a local mirror.", Example: "[{registry:registry.k8s.io,host:http://mirror.local:5000}]"},
			"spec.registryHosts[].registry":     {Description: "Registry namespace whose `hosts.toml` file will be written under `path`.", Example: "registry.k8s.io"},
			"spec.registryHosts[].server":       {Description: "Upstream registry server URL recorded in the generated `hosts.toml` file.", Example: "https://registry.k8s.io"},
			"spec.registryHosts[].host":         {Description: "Mirror host URL that containerd should contact for pulls and resolves.", Example: "http://registry.local:5000"},
			"spec.registryHosts[].capabilities": {Description: "Capabilities granted to the mirror host. Typical values are `pull` and `resolve`.", Example: "[pull,resolve]"},
			"spec.registryHosts[].skipVerify":   {Description: "Skip TLS verification for the mirror host. Useful for local mirrors with self-signed certificates.", Example: "true"},
		},
		Notes: []string{
			"Use this step when the runtime should resolve pulls through an internal mirror or custom trust policy.",
		},
	},

	"EditTOML": {
		Example: "kind: EditTOML\nspec:\n  path: /etc/containerd/config.toml\n  edits:\n    - op: set\n      rawPath: plugins.\"io.containerd.grpc.v1.cri\".registry.config_path\n      value: /etc/containerd/certs.d\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":            {Description: "TOML file path to edit in place.", Example: "/etc/containerd/config.toml"},
			"spec.createIfMissing": {Description: "Create a new empty TOML document when the file does not exist. Defaults to `false`.", Example: "true"},
			"spec.edits":           {Description: "Ordered list of structured edits applied sequentially to the TOML document.", Example: "[{op:set,rawPath:plugins.\"io.containerd.grpc.v1.cri\".registry.config_path,value:/etc/containerd/certs.d}]"},
			"spec.edits[].op":      {Description: "Structured edit operation. Use `set`, `delete`, `appendUnique`, or `replaceList`.", Example: "set"},
			"spec.edits[].rawPath": {Description: "Quoted-segment TOML path. Segments are dot-separated and keys containing dots may be quoted.", Example: "plugins.\"io.containerd.grpc.v1.cri\".registry.config_path"},
			"spec.edits[].value":   {Description: "Typed edit value. Supported values include string, boolean, number, list, object, and null depending on the operation.", Example: "/etc/containerd/certs.d"},
			"spec.mode":            {Description: "Optional file permissions to apply after the edit completes.", Example: "0644"},
		},
		Notes: []string{
			"Use this for generic TOML editing when a domain-specific step is unnecessary.",
		},
	},

	"EditYAML": {
		Example: "kind: EditYAML\nspec:\n  path: /etc/kubernetes/kubeadm-config.yaml\n  edits:\n    - op: set\n      rawPath: ClusterConfiguration.imageRepository\n      value: registry.local/k8s\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":            {Description: "YAML file path to edit in place.", Example: "/etc/kubernetes/kubeadm-config.yaml"},
			"spec.createIfMissing": {Description: "Create a new empty YAML document when the file does not exist. Defaults to `false`.", Example: "true"},
			"spec.edits":           {Description: "Ordered list of structured edits applied sequentially to the YAML document.", Example: "[{op:set,rawPath:spec.template.spec.containers.0.image,value:registry.local/app:v1}]"},
			"spec.edits[].op":      {Description: "Structured edit operation. Use `set`, `delete`, `appendUnique`, or `replaceList`.", Example: "set"},
			"spec.edits[].rawPath": {Description: "Dot-path with optional quoted key segments and numeric array indexes.", Example: "spec.template.spec.containers.0.image"},
			"spec.edits[].value":   {Description: "Typed edit value. Supported values include string, boolean, number, list, object, and null depending on the operation.", Example: "registry.local/k8s"},
			"spec.mode":            {Description: "Optional file permissions to apply after the edit completes.", Example: "0644"},
		},
		Notes: []string{
			"YAML support targets common map/list documents rather than advanced YAML features.",
			"Comment placement, anchors, aliases, merge keys, and style preservation are not guaranteed.",
		},
	},

	"EditJSON": {
		Example: "kind: EditJSON\nspec:\n  path: /etc/cni/net.d/10-custom.conflist\n  edits:\n    - op: set\n      rawPath: plugins.0.type\n      value: bridge\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":            {Description: "JSON file path to edit in place.", Example: "/etc/cni/net.d/10-custom.conflist"},
			"spec.createIfMissing": {Description: "Create a new empty JSON object when the file does not exist. Defaults to `false`.", Example: "true"},
			"spec.edits":           {Description: "Ordered list of structured edits applied sequentially to the JSON document.", Example: "[{op:set,rawPath:plugins.0.type,value:bridge}]"},
			"spec.edits[].op":      {Description: "Structured edit operation. Use `set`, `delete`, `appendUnique`, or `replaceList`.", Example: "set"},
			"spec.edits[].rawPath": {Description: "Dot-path with optional quoted key segments and numeric array indexes.", Example: "plugins.0.type"},
			"spec.edits[].value":   {Description: "Typed edit value. Supported values include string, boolean, number, list, object, and null depending on the operation.", Example: "bridge"},
			"spec.mode":            {Description: "Optional file permissions to apply after the edit completes.", Example: "0644"},
		},
		Notes: []string{
			"Use this when JSON configuration should be updated by path rather than rewritten entirely.",
		},
	},

	"EnsureDirectory": {
		Example: "kind: EnsureDirectory\nspec:\n  path: /home/vagrant/.kube\n  mode: \"0755\"\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path": {Description: "EnsureDirectory path to create if it does not already exist. Parent directories are created as needed.", Example: "/var/lib/deck"},
			"spec.mode": {Description: "EnsureDirectory permissions in octal notation. Applied after ensuring the directory exists, including on existing directories.", Example: "0755"},
		},
	},

	"File": {
		ActionExamples: map[string]string{
			"download":       "kind: File\nspec:\n  action: download\n  source:\n    url: https://mirror.example.com/runc\n  mode: \"0755\"\n",
			"write":          "kind: File\nspec:\n  action: write\n  path: /etc/containerd/config.toml\n  template: |\n    [plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n      SystemdCgroup = {{ .vars.systemdCgroup }}\n  mode: \"0644\"\n",
			"copy":           "kind: File\nspec:\n  action: copy\n  source:\n    path: /etc/kubernetes/admin.conf\n  path: /home/vagrant/.kube/config\n  mode: \"0644\"\n",
			"edit":           "kind: File\nspec:\n  action: edit\n  path: /etc/containerd/config.toml\n  edits:\n    - match: SystemdCgroup = false\n      replaceWith: SystemdCgroup = true\n",
			"extractArchive": "kind: File\nspec:\n  action: extractArchive\n  source:\n    path: /tmp/cni-plugins.tgz\n  path: /opt/cni/bin\n  include: [bridge, loopback]\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.path":                {Description: "Destination path on the node. Used by `write`, `copy`, `edit`, and `extractArchive`.", Example: "/etc/containerd/config.toml"},
			"spec.content":             {Description: "Inline file content written verbatim to `path`. Used with `write`.", Example: "[offline-base]\nbaseurl=http://repo.local"},
			"spec.template":            {Description: "Inline multi-line content rendered with the current vars before writing. Use this instead of `content` when the body includes template expressions such as `{{ .vars.* }}`.", Example: "[Service]\nEnvironment=ROLE={{ .vars.role }}"},
			"spec.mode":                {Description: "File permissions in octal notation applied after `write`, `copy`, `edit`, or `extractArchive` completes.", Example: "0644"},
			"spec.backup":              {Description: "Create a `.bak` copy of the original file before overwriting it.", Example: "true"},
			"spec.source":              {Description: "Structured source descriptor for download, copy, or archive extraction. `path`, `bundle`, or `url` may be used depending on the step.", Example: "{url:https://example.invalid/file.tar.gz}"},
			"spec.source.url":          {Description: "URL to fetch the file from during prepare.", Example: "https://mirror.example.com/runc"},
			"spec.source.path":         {Description: "Local filesystem path to use as the source. Applies to prepare downloads and apply-time copy or extraction when the source is already on disk.", Example: "/opt/cache/runc"},
			"spec.source.sha256":       {Description: "Expected SHA-256 checksum. Fails the step if the fetched file does not match.", Example: "abc123..."},
			"spec.source.bundle":       {Description: "Reference to a file already inside the bundle. Used to stage a bundle-resident file into a new output location.", Example: "{root:files,path:bin/linux/amd64/runc}"},
			"spec.source.bundle.root":  {Description: "Bundle root category to read from (`files`, `images`, or `packages`).", Example: "files"},
			"spec.source.bundle.path":  {Description: "Relative path within the bundle root to the source file.", Example: "bin/linux/amd64/runc"},
			"spec.outputPath":          {Description: "Optional prepare-side output path for a downloaded file written into bundle storage. Omit this to use `files/<basename>` based on the source file name, or set it when later steps need a stable custom path.", Example: "files/bin/runc"},
			"spec.include":             {Description: "Optional archive members to extract when using `ExtractArchive`. Extract all members when omitted.", Example: "[bridge,loopback]"},
			"spec.edits":               {Description: "Ordered list of match/replace rules applied sequentially to the file. Required for `edit`.", Example: "[{match:SystemdCgroup = false,replaceWith:SystemdCgroup = true}]"},
			"spec.edits[].match":       {Description: "Literal string or pattern to search for in the file.", Example: "SystemdCgroup = false"},
			"spec.edits[].replaceWith": {Description: "Replacement string. Substituted wherever `match` is found.", Example: "SystemdCgroup = true"},
			"spec.edits[].op":          {Description: "Edit operation type. `replace` substitutes all matches; `append` keeps the match text and adds `replaceWith` after each match. Defaults to `replace`.", Example: "replace"},
			"spec.fetch":               {Description: "Optional download transport settings applied to `DownloadFile` fetches.", Example: "{offlineOnly:true}"},
		},
		Notes: []string{
			"`DownloadFile` writes into prepared bundle storage through `outputPath`, while `WriteFile`, `CopyFile`, `EditFile`, and `ExtractArchive` operate on node paths through `path`.",
			"Omit `outputPath` unless you need a specific bundle location; deck defaults to `files/<basename>` for single-file downloads.",
			"Use `source.path` when the input is a simple local path and `source.bundle` or `source.url` when the source is structured or external.",
			"Use `template` instead of `content` when the body includes variable substitution.",
		},
	},

	"Image": {
		ActionExamples: map[string]string{
			"download": "kind: Image\nspec:\n  action: download\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n    - registry.example.com/platform/pause:3.9\n  auth:\n    - registry: registry.example.com\n      basic:\n        username: \"{{ .vars.registryUser }}\"\n        password: \"{{ .vars.registryPassword }}\"\n",
			"load":     "kind: Image\nspec:\n  action: load\n  sourceDir: images/control-plane\n  runtime: ctr\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n",
			"verify":   "kind: Image\nspec:\n  action: verify\n  command: [ctr, -n, k8s.io, images, list, -q]\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.images":                {Description: "Fully qualified image references to download, load, or verify.", Example: "[registry.k8s.io/pause:3.9]"},
			"spec.auth":                  {Description: "Optional registry authentication entries for `download`. Match each private registry with credentials while leaving public registries to the default keychain.", Example: "[{registry:registry.example.com,basic:{username:robot,password:${REGISTRY_PASSWORD}}}]"},
			"spec.auth[].registry":       {Description: "Registry host matched against each image reference, for example `registry.example.com` or `ghcr.io`.", Example: "registry.example.com"},
			"spec.auth[].basic":          {Description: "Explicit basic-auth credentials used when downloading from the matched registry.", Example: "{username:robot,password:${REGISTRY_PASSWORD}}"},
			"spec.auth[].basic.username": {Description: "Registry username used for basic authentication.", Example: "robot"},
			"spec.auth[].basic.password": {Description: "Registry password or access token paired with `basic.username`.", Example: "${REGISTRY_PASSWORD}"},
			"spec.outputDir":             {Description: "Optional bundle-relative directory where per-image tar archives are written during `DownloadImage`. Omit this to write under the default `images` root, or set it when you want a dedicated image subdirectory such as `images/control-plane`.", Example: "images/control-plane"},
			"spec.sourceDir":             {Description: "Directory containing prepared image archives to load into the runtime. Defaults to `images` when omitted.", Example: "images/control-plane"},
			"spec.runtime":               {Description: "Runtime loader used by `LoadImage`. `auto` picks the default runtime integration; explicit values include `ctr`, `docker`, and `podman`.", Example: "ctr"},
			"spec.command":               {Description: "Optional runtime command override. For `VerifyImage`, this is the image-listing command. For `LoadImage`, this command may include `{archive}` placeholders that deck substitutes per image archive.", Example: "[ctr,-n,k8s.io,images,list,-q]"},
			"spec.backend":               {Description: "Backend-specific download settings such as image transfer engine configuration. Applies to `DownloadImage` only.", Example: "{engine:go-containerregistry}"},
			"spec.backend.engine":        {Description: "Image download engine. Currently only `go-containerregistry` is supported.", Example: "go-containerregistry"},
		},
		Notes: []string{
			"Use `DownloadImage` during prepare, `LoadImage` during apply when archives must be imported, and `VerifyImage` when the runtime should already contain the required images.",
			"Omit `outputDir` unless you need a custom bundle subdirectory; deck writes to `images/` by default.",
			"Use explicit image tags or digests to keep prepared bundles reproducible.",
			"`spec.auth` is optional and only applies to `DownloadImage`; when omitted, deck falls back to the environment's default registry keychain.",
		},
	},

	"CheckHost": {
		Example: "kind: CheckHost\nspec:\n  checks: [os, arch, swap]\n  failFast: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.checks":   {Description: "Named checks to run. Supported values include `os`, `arch`, `swap`, `kernelModules`, and `binaries`.", Example: "[os,arch,swap]"},
			"spec.binaries": {Description: "Binary names to verify are present in `PATH`. Used when `host-check` includes `binaries`.", Example: "[kubeadm,kubelet,kubectl]"},
			"spec.failFast": {Description: "Stop on the first failing check rather than running all checks. Defaults to `true`.", Example: "true"},
		},
	},

	"KernelModule": {
		Example: "kind: KernelModule\nspec:\n  name: br_netfilter\n  load: true\n  persist: true\n  persistFile: /etc/modules-load.d/k8s.conf\n",
		FieldDocs: map[string]FieldDoc{
			"spec.name":        {Description: "Single module name to load. Use `name` or `names`, not both.", Example: "br_netfilter"},
			"spec.names":       {Description: "Multiple module names to load in a single step. Use `name` or `names`, not both.", Example: "[overlay,br_netfilter]"},
			"spec.load":        {Description: "Run `modprobe` to load the module immediately. Defaults to `true`.", Example: "true"},
			"spec.persist":     {Description: "Write the module name to a file under `/etc/modules-load.d/` so it is loaded on every boot. Defaults to `true`.", Example: "true"},
			"spec.persistFile": {Description: "Path to the persistence file written when `persist` is true.", Example: "/etc/modules-load.d/k8s.conf"},
		},
	},

	"CheckCluster": {
		Example: "kind: CheckCluster\nspec:\n  interval: 5s\n  nodes:\n    total: 1\n    ready: 1\n    controlPlaneReady: 1\n  reports:\n    nodesPath: /tmp/deck/reports/bootstrap-nodes.txt\n",
		FieldDocs: map[string]FieldDoc{
			"spec.kubeconfig":                                {Description: "Kubeconfig path used for kubectl-based checks. Defaults to `/etc/kubernetes/admin.conf`.", Example: "/etc/kubernetes/admin.conf"},
			"spec.interval":                                  {Description: "Duration between poll attempts while waiting for cluster state to converge.", Example: "5s"},
			"spec.initialDelay":                              {Description: "Optional delay before the first poll attempt.", Example: "10s"},
			"spec.timeout":                                   {Description: "Maximum total duration to keep polling before the step fails.", Example: "10m"},
			"spec.nodes":                                     {Description: "Optional checks for cluster node count and readiness.", Example: "{total:1,ready:1,controlPlaneReady:1}"},
			"spec.nodes.total":                               {Description: "Expected total node count returned by `kubectl get nodes`.", Example: "1"},
			"spec.nodes.ready":                               {Description: "Expected count of Ready nodes.", Example: "1"},
			"spec.nodes.controlPlaneReady":                   {Description: "Expected count of Ready control-plane nodes.", Example: "1"},
			"spec.versions":                                  {Description: "Optional checks for Kubernetes component versions and an optional version report.", Example: "{server:v1.31.0,kubelet:v1.31.0}"},
			"spec.versions.targetVersion":                    {Description: "Target Kubernetes version written into the optional version report file.", Example: "v1.31.0"},
			"spec.versions.server":                           {Description: "Expected API server version from `kubectl version -o json`.", Example: "v1.31.0"},
			"spec.versions.kubelet":                          {Description: "Expected kubelet version for the selected node.", Example: "v1.31.0"},
			"spec.versions.kubeadm":                          {Description: "Expected local kubeadm version from `kubeadm version -o short`.", Example: "v1.31.0"},
			"spec.versions.nodeName":                         {Description: "Node name used when reading kubelet version. Defaults to `control-plane`.", Example: "control-plane"},
			"spec.versions.reportPath":                       {Description: "Optional report file that records target, server, kubelet, and kubeadm versions.", Example: "/tmp/deck/reports/upgrade-version.txt"},
			"spec.kubeSystem":                                {Description: "Optional checks for `kube-system` pod readiness and optional pod state reports.", Example: "{readyNames:[etcd-control-plane]}"},
			"spec.kubeSystem.readyNames":                     {Description: "Exact kube-system pod names that must be present and fully Ready.", Example: "[etcd-control-plane,kube-apiserver-control-plane]"},
			"spec.kubeSystem.readyPrefixes":                  {Description: "Pod-name prefixes for which at least one matching Ready pod must exist.", Example: "[kube-proxy-]"},
			"spec.kubeSystem.readyPrefixMinimums":            {Description: "Prefix-based readiness requirements with minimum Ready pod counts.", Example: "[{prefix:coredns-,minReady:2}]"},
			"spec.kubeSystem.readyPrefixMinimums[].prefix":   {Description: "Pod-name prefix to match inside `kube-system`.", Example: "coredns-"},
			"spec.kubeSystem.readyPrefixMinimums[].minReady": {Description: "Minimum number of matching Ready pods required for the prefix.", Example: "2"},
			"spec.kubeSystem.reportPath":                     {Description: "Optional text report path for `kubectl get pods -n kube-system`.", Example: "/tmp/deck/reports/kube-system-pods.txt"},
			"spec.kubeSystem.jsonReportPath":                 {Description: "Optional JSON report path for `kubectl get pods -n kube-system -o json`.", Example: "/tmp/deck/reports/kube-system-pods.json"},
			"spec.fileAssertions":                            {Description: "Optional file-content assertions evaluated on every poll attempt.", Example: "[{path:/etc/containerd/config.toml,contains:[registry.k8s.io/pause:3.10]}]"},
			"spec.fileAssertions[].path":                     {Description: "Path of the local file whose content should be checked.", Example: "/etc/containerd/config.toml"},
			"spec.fileAssertions[].contains":                 {Description: "Strings that must all be present in the file content.", Example: "[registry.k8s.io/pause:3.10]"},
			"spec.reports":                                   {Description: "Optional paths for writing node and cluster state reports during verification.", Example: "{nodesPath:/tmp/deck/reports/bootstrap-nodes.txt}"},
			"spec.reports.nodesPath":                         {Description: "Optional report file path for `kubectl get nodes` output.", Example: "/tmp/deck/reports/bootstrap-nodes.txt"},
			"spec.reports.clusterNodesPath":                  {Description: "Optional second node report path when the workflow wants both scenario-specific and shared cluster node reports.", Example: "/tmp/deck/reports/cluster-nodes.txt"},
		},
	},

	"Kubeadm": {
		ActionExamples: map[string]string{
			"init":    "kind: Kubeadm\nspec:\n  outputJoinFile: /tmp/deck/join.txt\n  podNetworkCIDR: 10.244.0.0/16\n",
			"join":    "kind: Kubeadm\nspec:\n  configFile: /tmp/deck/kubeadm-join.yaml\n  extraArgs: [--skip-phases=preflight]\n",
			"reset":   "kind: Kubeadm\nspec:\n  force: true\n  removePaths: [/etc/cni/net.d, /var/lib/etcd]\n",
			"upgrade": "kind: UpgradeKubeadm\nspec:\n  kubernetesVersion: v1.31.0\n  ignorePreflightErrors: [Swap]\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.outputJoinFile":              {Description: "Path where the generated join command is written after `InitKubeadm`. Worker nodes read this file to join the cluster.", Example: "/tmp/deck/join.txt"},
			"spec.joinFile":                    {Description: "Path to the join command file produced by a prior `InitKubeadm` run. For `JoinKubeadm`, provide this or `configFile`.", Example: "/tmp/deck/join.txt"},
			"spec.configFile":                  {Description: "Path to an explicit kubeadm config file passed with `--config`. For `JoinKubeadm`, provide this or `joinFile`. For `InitKubeadm`, combine it with `configTemplate` or a pre-rendered kubeadm config.", Example: "/tmp/deck/kubeadm.conf"},
			"spec.configTemplate":              {Description: "For `InitKubeadm`, use `default` for the deck-managed kubeadm config template. Any other non-empty value is written literally as inline kubeadm YAML content to `configFile`.", Example: "default"},
			"spec.kubernetesVersion":           {Description: "Kubernetes version string passed to kubeadm. Accepts the `{{ .vars.* }}` template syntax.", Example: "v1.30.1"},
			"spec.advertiseAddress":            {Description: "API server advertise address for `InitKubeadm`. Use `auto` to detect the primary interface, or provide an explicit IP.", Example: "auto"},
			"spec.podNetworkCIDR":              {Description: "CIDR range for the pod network passed to `InitKubeadm`. Must not overlap with node or service CIDRs.", Example: "10.244.0.0/16"},
			"spec.criSocket":                   {Description: "CRI socket path passed to kubeadm. Required when multiple container runtimes are installed on the node.", Example: "unix:///run/containerd/containerd.sock"},
			"spec.ignorePreflightErrors":       {Description: "Kubeadm preflight check names to suppress. Use sparingly and only for known-safe deviations.", Example: "[swap]"},
			"spec.extraArgs":                   {Description: "Additional flags passed directly to the kubeadm subcommand as `--key=value` pairs.", Example: "[--skip-phases=addon/kube-proxy]"},
			"spec.skipIfAdminConfExists":       {Description: "Skip the `InitKubeadm` step if `/etc/kubernetes/admin.conf` already exists, treating the node as already bootstrapped. Defaults to `true`.", Example: "true"},
			"spec.asControlPlane":              {Description: "When `true`, adds `--control-plane` so the node joins as an additional control-plane member rather than a worker.", Example: "false"},
			"spec.force":                       {Description: "Pass `--force` to `ResetKubeadm` to skip interactive confirmation prompts.", Example: "true"},
			"spec.ignoreErrors":                {Description: "For `ResetKubeadm`, continue with filesystem and runtime cleanup even if the kubeadm command itself fails. Later cleanup steps still fail the step if they error.", Example: "true"},
			"spec.stopKubelet":                 {Description: "Stop the kubelet service before running `ResetKubeadm`. Defaults to `true`.", Example: "true"},
			"spec.removePaths":                 {Description: "Directories to delete during `ResetKubeadm` cleanup, such as CNI and etcd data.", Example: "[/etc/cni/net.d,/var/lib/etcd]"},
			"spec.removeFiles":                 {Description: "Individual files to delete during `ResetKubeadm` cleanup, such as kubeconfig files.", Example: "[/etc/kubernetes/admin.conf]"},
			"spec.cleanupContainers":           {Description: "Container names to stop and remove during `ResetKubeadm`. Useful when the runtime has stale control-plane containers.", Example: "[kube-apiserver,etcd]"},
			"spec.restartRuntimeService":       {Description: "Container runtime service name to restart after `ResetKubeadm` cleanup completes.", Example: "containerd"},
			"spec.waitForRuntimeService":       {Description: "After restarting the runtime service, wait until systemd reports it active.", Example: "true"},
			"spec.waitForRuntimeReady":         {Description: "After reset cleanup, poll `crictl info` until the runtime responds successfully.", Example: "true"},
			"spec.waitForMissingManifestsGlob": {Description: "Glob that must resolve to zero matches before the reset step succeeds. Useful for static pod manifest cleanup.", Example: "/etc/kubernetes/manifests/*.yaml"},
			"spec.stopKubeletAfterReset":       {Description: "Stop kubelet again after runtime convergence and verification complete.", Example: "true"},
			"spec.verifyContainersAbsent":      {Description: "Container names that must no longer exist after reset cleanup.", Example: "[kube-apiserver,etcd]"},
			"spec.reportFile":                  {Description: "Optional reset-proof report file written after cleanup and verification complete.", Example: "/tmp/deck/reports/reset-state.txt"},
			"spec.reportResetReason":           {Description: "Value written into the reset report as `resetReason`.", Example: "node-reset-acceptance"},
			"spec.restartKubelet":              {Description: "Restart the kubelet service after a successful `UpgradeKubeadm` run. Defaults to `true`.", Example: "true"},
			"spec.kubeletService":              {Description: "Service name restarted after a successful `UpgradeKubeadm`. Defaults to `kubelet`.", Example: "kubelet"},
		},
		Notes: []string{
			"`InitKubeadm` requires `outputJoinFile`, `JoinKubeadm` requires exactly one of `joinFile` or `configFile`, `ResetKubeadm` focuses on cleanup fields, and `UpgradeKubeadm` performs local control-plane upgrades.",
			"When `skipIfAdminConfExists` skips `InitKubeadm`, deck does not create a new join artifact and registered `joinFile` outputs are unavailable unless the file already exists.",
			"Load prepared control-plane images with `LoadImage` before `InitKubeadm` instead of relying on kubeadm image pulls hidden inside the bootstrap step.",
			"Place host preparation steps (`WriteContainerdConfig`, `Swap`, `KernelModule`, `Sysctl`) before kubeadm bootstrap so failures point to the correct step.",
		},
	},

	"RefreshRepository": {
		Example: "kind: RefreshRepository\nspec:\n  manager: apt\n  clean: true\n  update: true\n  restrictToRepos:\n    - /etc/apt/sources.list.d/offline.list\n",
		FieldDocs: map[string]FieldDoc{
			"spec.manager":         {Description: "Package manager to use. `auto` detects from the host OS. Supports `apt` and `dnf`.", Example: "apt"},
			"spec.clean":           {Description: "Run a cache clean before updating metadata (`apt clean` / `dnf clean all`).", Example: "true"},
			"spec.update":          {Description: "Fetch fresh package metadata from the configured repositories (`apt update` / `dnf makecache`).", Example: "true"},
			"spec.restrictToRepos": {Description: "Limit the metadata update to these repository selectors. For apt, use repo file paths or globs; for dnf, use repo IDs. Prevents fetching from online repos during an offline install.", Example: "[/etc/apt/sources.list.d/offline.list]"},
			"spec.excludeRepos":    {Description: "Repository selectors to skip during metadata update. For apt, selectors match repo file paths; for dnf, they match repo IDs.", Example: "[updates]"},
		},
	},

	"Package": {
		ActionExamples: map[string]string{
			"download": "kind: Package\nspec:\n  packages: [podman]\n  distro:\n    family: rhel\n    release: rocky9\n  repo:\n    type: rpm\n    modules:\n      - name: container-tools\n        stream: \"4.0\"\n  backend:\n    mode: container\n    runtime: docker\n    image: rockylinux:9\n",
			"install":  "kind: Package\nspec:\n  packages: [kubelet, kubeadm, kubectl]\n  source:\n    type: local-repo\n    path: /opt/deck/repos/kubernetes\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.packages":              {Description: "Package names to download or install. Use the same list in both `download` and `install` steps to keep offline parity.", Example: "[kubelet,kubeadm,kubectl]"},
			"spec.source":                {Description: "Local repository source for `InstallPackage`. Points to a pre-prepared on-disk package repo instead of relying on configured package manager sources.", Example: "{type:local-repo,path:/opt/deck/repos/kubernetes}"},
			"spec.source.type":           {Description: "Source type. Currently `local-repo` is the only supported value.", Example: "local-repo"},
			"spec.source.path":           {Description: "Filesystem path to the pre-prepared local package repository.", Example: "/opt/deck/repos/kubernetes"},
			"spec.restrictToRepos":       {Description: "For `InstallPackage`, limit package manager visibility to these repository selectors. For deb-family systems, use repo file paths or globs; for rpm-family systems, use repo IDs.", Example: "[offline-kubernetes]"},
			"spec.excludeRepos":          {Description: "For `InstallPackage`, repository selectors to exclude from package resolution. For deb-family systems, selectors match repo file paths; for rpm-family systems, they match repo IDs.", Example: "[updates]"},
			"spec.distro":                {Description: "Target distribution hint used by `DownloadPackage` to select the correct package manager and resolver backend.", Example: "{family:rhel,release:rocky9}"},
			"spec.repo":                  {Description: "Package repository settings applied before `DownloadPackage`, including repo layout generation and RPM module streams.", Example: "{type:rpm,modules:[...]}"},
			"spec.repo.type":             {Description: "Repository output type for `DownloadPackage` repo mode. Supported values are `deb-flat` and `rpm`.", Example: "rpm"},
			"spec.repo.generate":         {Description: "When `true`, generate repository metadata after the package payload is collected. Used with `repo.type` in download repo mode.", Example: "true"},
			"spec.repo.pkgsDir":          {Description: "Subdirectory under the generated repo root where package payloads are written. Defaults to `pkgs`.", Example: "pkgs"},
			"spec.repo.modules":          {Description: "RPM module streams to enable before resolving downloads on RHEL-family systems.", Example: "[{name:container-tools,stream:4.0}]"},
			"spec.repo.modules[].name":   {Description: "RPM module name to enable.", Example: "container-tools"},
			"spec.repo.modules[].stream": {Description: "Module stream version paired with the module name.", Example: "4.0"},
			"spec.backend":               {Description: "Container-based download backend for `DownloadPackage`. When provided, `backend.mode=container` and `backend.image` are required.", Example: "{mode:container,runtime:docker,image:rockylinux:9}"},
			"spec.backend.mode":          {Description: "Download backend mode. Currently only `container` is supported.", Example: "container"},
			"spec.backend.runtime":       {Description: "Preferred container runtime for the download helper container. Supported values are `docker`, `podman`, or `auto`.", Example: "docker"},
			"spec.backend.image":         {Description: "Container image used for package resolution in `download` mode. Required when `backend` is set.", Example: "rockylinux:9"},
			"spec.outputDir":             {Description: "Optional bundle-relative directory used by `DownloadPackage` for downloaded package artifacts. Omit this to use `packages/` by default. When `repo.type` is set, deck instead writes to a repo layout under `packages/deb/<release>` or `packages/rpm/<release>`. Set `outputDir` only when apply workflows need a stable custom path outside those defaults.", Example: "packages/kubernetes"},
		},
		Notes: []string{
			"Use `DownloadPackage` and `InstallPackage` with `ConfigureRepository` and `RefreshRepository` for a complete typed package-management flow.",
			"Omit `outputDir` unless you need a custom package location; deck uses `packages/` by default, or `packages/deb/<release>` and `packages/rpm/<release>` when `repo.type` is set.",
			"Keeping the same package list across `download` and `install` helps maintain offline parity.",
			"Use `restrictToRepos` on the `InstallPackage` step to prevent the node's default online repos from being consulted during an offline apply.",
			"When `repo` is set for `DownloadPackage`, deck expects `repo.type` and `distro.release` so it can build a `deb-flat` or `rpm` repository layout.",
			"Container-backed `DownloadPackage` exports completed artifacts into a host-owned cache and does not bind-mount deb/rpm package-manager cache directories.",
			"Older releases may have left root-owned content under legacy package cache paths; clean those directories manually after upgrading if needed.",
			"Without a container download backend, `download` currently writes placeholder package markers instead of resolving real packages.",
		},
	},

	"Repository": {
		ActionExamples: map[string]string{
			"configure": "kind: ConfigureRepository\nspec:\n  format: deb\n  path: /etc/apt/sources.list.d/offline.list\n  repositories:\n    - baseurl: http://repo.local/debian\n      trusted: true\n",
			"refresh":   "kind: RefreshRepository\nspec:\n  manager: apt\n  clean: true\n  update: true\n  restrictToRepos:\n    - /etc/apt/sources.list.d/offline.list\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.format":               {Description: "Repository file format to write. `auto` detects from the host family, `deb` produces a sources.list style entry, and `rpm` produces a `.repo` file.", Example: "deb"},
			"spec.path":                 {Description: "Explicit output path for the generated repository file. Defaults to `/etc/apt/sources.list.d/deck-offline.list` for deb-family systems or `/etc/yum.repos.d/deck-offline.repo` for rpm-family systems when omitted.", Example: "/etc/apt/sources.list.d/offline.list"},
			"spec.mode":                 {Description: "File permissions applied to the generated repository file in octal notation.", Example: "0644"},
			"spec.replaceExisting":      {Description: "Replace an existing repository file at the target path before writing the new definition.", Example: "true"},
			"spec.disableExisting":      {Description: "Disable all existing repository definitions before writing the new one. Prevents conflicts from online repos during offline installs.", Example: "true"},
			"spec.backupPaths":          {Description: "Paths to back up before modifying. Backed-up files are saved with a `.bak` suffix.", Example: "[/etc/apt/sources.list]"},
			"spec.cleanupPaths":         {Description: "Paths to remove before writing the new repository definition.", Example: "[/etc/apt/sources.list.d/ubuntu.list]"},
			"spec.repositories":         {Description: "Repository entries to write. deb entries use fields like `baseurl`, `suite`, `component`, and optional `trusted`; rpm entries use fields like `id`, `name`, `baseurl`, and optional `extra` for additional repo keys.", Example: "[{baseurl:http://repo.local/debian,trusted:true}]"},
			"spec.repositories[].extra": {Description: "Additional rpm-style repository key-value pairs written after the typed fields. Use this sparingly for repository-specific options not modeled directly.", Example: "{priority:10,module_hotfixes:true}"},
			"spec.manager":              {Description: "Package manager to use for repository metadata refresh. `auto` detects from the host OS. Supports `apt` and `dnf`.", Example: "apt"},
			"spec.clean":                {Description: "Run a cache clean before updating metadata (`apt clean` / `dnf clean all`).", Example: "true"},
			"spec.update":               {Description: "Fetch fresh package metadata from the configured repositories (`apt update` / `dnf makecache`).", Example: "true"},
			"spec.restrictToRepos":      {Description: "Limit the metadata update to these repository selectors. For apt, use repo file paths or globs; for dnf, use repo IDs. Prevents fetching from online repos during an offline install.", Example: "[/etc/apt/sources.list.d/offline.list]"},
			"spec.excludeRepos":         {Description: "Repository selectors to skip during metadata update. For apt, selectors match repo file paths; for dnf, they match repo IDs.", Example: "[updates]"},
		},
		Notes: []string{
			"`ConfigureRepository` only writes repository definition files. Use `RefreshRepository` when the package manager needs an explicit metadata refresh.",
			"Keep repository definitions mirror-specific rather than mutating the host's default online sources.",
		},
	},

	"ManageService": {
		Example: "kind: ManageService\nspec:\n  name: containerd\n  enabled: true\n  state: started\n",
		FieldDocs: map[string]FieldDoc{
			"spec.name":          {Description: "Single service name to manage. Use `name` or `names`, not both.", Example: "containerd"},
			"spec.names":         {Description: "Multiple service names to manage in one step. Use `name` or `names`, not both.", Example: "[firewalld,ufw]"},
			"spec.state":         {Description: "Desired service state. `started` ensures the service is running; `stopped` ensures it is not; `restarted` forces a restart; `reloaded` sends SIGHUP; `unchanged` skips state management.", Example: "started"},
			"spec.enabled":       {Description: "Whether the service should be enabled to start on boot.", Example: "true"},
			"spec.daemonReload":  {Description: "Run `systemctl daemon-reload` before applying state changes. Required after writing new unit files.", Example: "true"},
			"spec.ifExists":      {Description: "Only manage the service if it exists on the host. Prevents failures on optional services.", Example: "true"},
			"spec.ignoreMissing": {Description: "Suppress errors when the service is not found. Use with `ifExists` for optional services.", Example: "true"},
		},
	},

	"Swap": {
		Example: "kind: Swap\nspec:\n  disable: true\n  persist: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.disable":   {Description: "Disable all active swap devices with `swapoff -a`. Defaults to `true`.", Example: "true"},
			"spec.persist":   {Description: "Comment out swap entries in `/etc/fstab` so swap stays off after reboot. Defaults to `true`.", Example: "true"},
			"spec.fstabPath": {Description: "Path to the fstab file. Defaults to `/etc/fstab`.", Example: "/etc/fstab"},
		},
	},

	"CreateSymlink": {
		Example: "kind: CreateSymlink\nspec:\n  path: /usr/bin/runc\n  target: /usr/local/sbin/runc\n  force: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":                {Description: "Path where the symbolic link will be created.", Example: "/usr/bin/runc"},
			"spec.target":              {Description: "Path that the symbolic link points to.", Example: "/usr/local/sbin/runc"},
			"spec.force":               {Description: "Remove an existing file or link at `path` before creating the new link. Defaults to `false`.", Example: "true"},
			"spec.createParent":        {Description: "Create parent directories for `path` if they do not exist. Defaults to `false`.", Example: "true"},
			"spec.requireTarget":       {Description: "Fail the step if `target` does not exist at the time the link is created. Defaults to `false`.", Example: "true"},
			"spec.ignoreMissingTarget": {Description: "Treat a missing target as a no-op instead of an error. Useful when a compatibility symlink should only be created if the target path exists.", Example: "true"},
		},
	},

	"Sysctl": {
		Example: "kind: Sysctl\nspec:\n  writeFile: /etc/sysctl.d/99-kubernetes-cri.conf\n  apply: true\n  values:\n    net.ipv4.ip_forward: 1\n",
		FieldDocs: map[string]FieldDoc{
			"spec.writeFile": {Description: "Path to the sysctl file written with the given values. A drop-in under `/etc/sysctl.d/` is the common choice.", Example: "/etc/sysctl.d/99-k8s.conf"},
			"spec.values":    {Description: "Map of sysctl key-value pairs to write and optionally apply.", Example: "{net.ipv4.ip_forward:1,net.bridge.bridge-nf-call-iptables:1}"},
			"spec.apply":     {Description: "Run `sysctl -p <writeFile>` after writing the file to apply the values immediately without a reboot. Defaults to `false`.", Example: "true"},
		},
	},

	"WriteSystemdUnit": {
		Example: "kind: WriteSystemdUnit\nspec:\n  path: /etc/systemd/system/kubelet.service\n  template: |\n    [Unit]\n    Description=Kubelet\n\n    [Service]\n    Environment=NODE_IP={{ .vars.nodeIP }}\n  daemonReload: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":         {Description: "Destination path for the unit file on the node.", Example: "/etc/systemd/system/kubelet.service"},
			"spec.content":      {Description: "Inline unit file content written verbatim to `path`.", Example: "[Unit]\nDescription=kubelet"},
			"spec.template":     {Description: "Inline multi-line unit content rendered with the current vars before writing. Prefer this for parameterized unit files.", Example: "[Service]\nEnvironment=NODE_IP={{ .vars.nodeIP }}"},
			"spec.mode":         {Description: "File permissions applied to the unit file in octal notation.", Example: "0644"},
			"spec.daemonReload": {Description: "Run `systemctl daemon-reload` after writing the unit file so systemd picks up the change.", Example: "true"},
		},
		Notes: []string{
			"`WriteSystemdUnit` only writes the unit file and optionally performs `daemonReload`.",
			"Use `ManageService` separately to enable, start, restart, or reload the unit after it is written.",
		},
	},

	"Wait": {
		ActionExamples: map[string]string{
			"serviceActive":  "kind: Wait\nspec:\n  name: containerd\n  interval: 2s\n  timeout: 2m\n",
			"commandSuccess": "kind: Wait\nspec:\n  command: [test, -f, /etc/kubernetes/admin.conf]\n  interval: 2s\n  timeout: 2m\n",
			"fileExists":     "kind: Wait\nspec:\n  path: /etc/kubernetes/admin.conf\n  type: file\n  nonEmpty: true\n  interval: 2s\n  timeout: 5m\n",
			"fileAbsent":     "kind: Wait\nspec:\n  path: /var/lib/etcd/member\n  interval: 2s\n  timeout: 2m\n",
			"tcpPortOpen":    "kind: Wait\nspec:\n  port: \"6443\"\n  interval: 2s\n  timeout: 5m\n",
			"tcpPortClosed":  "kind: Wait\nspec:\n  port: \"10250\"\n  interval: 2s\n  timeout: 2m\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.name":         {Description: "Service name to check. Required for `WaitForService`.", Example: "containerd"},
			"spec.command":      {Description: "Command vector to run on each poll attempt. Required for `WaitForCommand`. The step succeeds when the command exits 0.", Example: "[test,-f,/etc/kubernetes/admin.conf]"},
			"spec.path":         {Description: "Filesystem path to check. Required for `WaitForFile` and `WaitForMissingFile`.", Example: "/etc/kubernetes/admin.conf"},
			"spec.paths":        {Description: "For `WaitForMissingFile`, require every listed path to be absent before the step succeeds.", Example: "[/etc/kubernetes/manifests/a.yaml,/etc/kubernetes/manifests/b.yaml]"},
			"spec.glob":         {Description: "For `WaitForMissingFile`, require the glob to resolve to zero matches before the step succeeds.", Example: "/etc/kubernetes/manifests/*.yaml"},
			"spec.type":         {Description: "Restricts the path check to a specific filesystem entry type. `file` matches regular files only, `dir` matches directories, `any` matches either. Defaults to `any`.", Example: "file"},
			"spec.nonEmpty":     {Description: "For `WaitForFile`, also assert that the file has non-zero size. Useful when waiting for a file that is written progressively.", Example: "true"},
			"spec.port":         {Description: "TCP port number to check. Required for `WaitForTCPPort` and `WaitForMissingTCPPort`.", Example: "6443"},
			"spec.address":      {Description: "Host or IP address for TCP port checks. Defaults to `127.0.0.1` when omitted.", Example: "127.0.0.1"},
			"spec.interval":     {Description: "Duration between poll attempts. Accepts Go duration strings.", Example: "2s"},
			"spec.initialDelay": {Description: "Duration to wait before the first poll attempt. Useful when a service needs a moment before it becomes checkable.", Example: "1s"},
			"spec.timeout":      {Description: "Maximum total duration to wait before failing the step.", Example: "5m"},
			"spec.pollInterval": {Description: "Deprecated alias for `interval`. Use `interval` instead.", Example: "2s"},
		},
		Notes: []string{
			"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.",
			"Keep waits specific so failures identify exactly which dependency did not become ready within the timeout.",
			"Use `initialDelay` when a service emits a transient non-active state immediately after being started.",
		},
	},
}

func ToolMetaForDefinition(def workflowcontract.StepDefinition) ToolMetadata {
	kind := def.Kind
	meta, ok := toolMetadata[kind]
	if !ok {
		meta, ok = toolMetadata[def.FamilyTitle]
	}
	if !ok {
		return ToolMetadata{Kind: kind, Category: "other", Summary: "Generated schema reference.", WhenToUse: "Use this schema according to the workflow contract."}
	}
	meta.Kind = kind
	meta.Category = def.Category
	meta.Summary = def.Summary
	meta.WhenToUse = def.WhenToUse
	meta.Example = normalizedToolExample(kind, def, meta)
	// Merge common field docs so every tool page documents shared execution controls.
	merged := make(map[string]FieldDoc, len(commonFieldDocs)+len(meta.FieldDocs))
	maps.Copy(merged, commonFieldDocs)
	maps.Copy(merged, meta.FieldDocs)
	delete(merged, "spec.action")
	meta.FieldDocs = merged
	return meta
}

func normalizedToolExample(kind string, def workflowcontract.StepDefinition, meta ToolMetadata) string {
	example := strings.TrimSpace(meta.Example)
	if example == "" {
		actionKey := legacyActionName(def)
		if actionKey != "" {
			example = strings.TrimSpace(meta.ActionExamples[actionKey])
		}
	}
	if example == "" {
		return ""
	}
	legacyKind := kind
	if strings.TrimSpace(def.FamilyTitle) != "" {
		legacyKind = def.FamilyTitle
	}
	return normalizeLegacyExample(example, legacyKind, kind)
}

func legacyActionName(def workflowcontract.StepDefinition) string {
	if def.Kind == def.FamilyTitle {
		return ""
	}
	base := strings.TrimSuffix(def.SchemaFile, ".schema.json")
	_, action, ok := strings.Cut(base, ".")
	if !ok {
		return ""
	}
	parts := strings.Split(action, "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func normalizeLegacyExample(example, legacyKind, kind string) string {
	lines := strings.Split(strings.TrimSpace(example), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "action:") {
			continue
		}
		filtered = append(filtered, strings.Replace(line, "kind: "+legacyKind, "kind: "+kind, 1))
	}
	return strings.Join(filtered, "\n") + "\n"
}

func ToolKinds() []string {
	defs := workflowcontract.StepDefinitions()
	kinds := make([]string, 0, len(defs))
	for _, def := range defs {
		kinds = append(kinds, def.Kind)
	}
	sort.Strings(kinds)
	return kinds
}

func WorkflowMeta() PageMetadata {
	return PageMetadata{
		Title:   "Workflow Schema",
		Summary: "Top-level workflow authoring reference for deck workflows.",
		Example: "version: v1alpha1\nsteps:\n  - id: write-config\n    apiVersion: deck/v1alpha1\n    kind: WriteFile\n    spec:\n      path: /etc/example.conf\n      content: hello\n",
		FieldDocs: map[string]FieldDoc{
			"phases":                  {Description: "Ordered execution phases. Each phase can contain imports, steps, or both.", Example: "[{name:install,steps:[...]}]"},
			"steps":                   {Description: "Flat step list for workflows that do not need named phases. Execution normalizes these steps into an implicit `default` phase.", Example: "[{id:configure-runtime,kind:WriteContainerdConfig,spec:{...}}]"},
			"steps[].kind":            {Description: "Typed step kind selected from the shipped public step inventory. Deck resolves step identity from effective `apiVersion` plus `kind`.", Example: "WriteFile"},
			"steps[].spec":            {Description: "Step payload validated against the schema for the resolved step identity.", Example: "{path:/etc/example.conf,content:hello}"},
			"steps[].when":            {Description: workflowcontract.WhenDescription(), Example: `vars.skipSetup != "true"`},
			"steps[].parallelGroup":   {Description: "Optional batch label. Consecutive steps with the same value in a phase may run in parallel and publish register outputs only after the whole batch succeeds.", Example: "downloads"},
			"steps[].retry":           {Description: "Number of times to retry the step after a failure before marking it as failed.", Example: "3"},
			"steps[].timeout":         {Description: "Maximum duration for the step. Accepts Go duration strings.", Example: "5m"},
			"steps[].register":        {Description: workflowcontract.RegisterDescription(), Example: workflowcontract.RegisterExample()},
			"phases[].name":           {Description: "Stable phase name used for ordering, selective execution, and persisted resume boundaries.", Example: "install"},
			"phases[].maxParallelism": {Description: "Optional cap for concurrently running steps inside a parallel batch in this phase.", Example: "2"},
			"phases[].imports":        {Description: "Component fragment imports that expand into this phase before step execution.", Example: "[{path:k8s/containerd-kubelet.yaml}]"},
		},
		Notes: []string{
			"A workflow must define at least one of `phases` or `steps`.",
			"A workflow cannot define both top-level `phases` and top-level `steps` at the same time.",
			"Top-level `steps` execute as an implicit phase named `default`.",
			"Imports are only supported under `phases[].imports` and resolve from `workflows/components/`.",
			"When a step omits `apiVersion`, deck resolves it from the top-level workflow `version` before schema and role checks run.",
			"Workflow mode is determined by command context or file location, not by an in-file `role` field.",
			"Each step still validates against its own kind-specific schema after the top-level workflow schema passes.",
		},
	}
}

func ToolDefinitionMeta() PageMetadata {
	return PageMetadata{
		Title:   "Tool Definition Schema",
		Summary: "Reference for tool definition manifests used to describe typed workflow tools.",
		Example: "apiVersion: deck/v1\nkind: ToolDefinition\nmetadata:\n  name: File\n  description: Manage files on the local node\nspec:\n  version: v1\n  summary: Manage files on the local node\n  category: shared\n  offlineAllowed: true\n  requires:\n    root: true\n  inputSchema: {}\n  idempotency:\n    mode: by-output\n    keys: [path]\n",
		FieldDocs: map[string]FieldDoc{
			"metadata.name":                  {Description: "Public tool kind name described by this manifest.", Example: "WriteFile"},
			"spec.version":                   {Description: "Manifest version for the tool definition contract.", Example: "v1"},
			"spec.summary":                   {Description: "Short product-facing summary of the tool's purpose.", Example: "Manage files on the local node."},
			"spec.category":                  {Description: "High-level grouping used for docs and discovery.", Example: "shared"},
			"spec.inputSchema":               {Description: "Schema fragment describing the tool's input contract.", Example: "{}"},
			"spec.outputContract":            {Description: "Declared outputs or state keys produced by the tool.", Example: "{artifacts:[joinFile],stateKeys:[nodeReady]}"},
			"spec.idempotency.mode":          {Description: "How callers should reason about repeated runs of the tool.", Example: "by-output"},
			"spec.requires.containerRuntime": {Description: "Whether the tool expects a container runtime on the node.", Example: "required"},
		},
		Notes: []string{
			"`ToolDefinition` describes a typed tool contract rather than a workflow step instance.",
			"`inputSchema` stays open-ended because it embeds another schema fragment rather than a fixed workflow payload.",
		},
	}
}

func ComponentFragmentMeta() PageMetadata {
	return PageMetadata{
		Title:   "Component Fragment Schema",
		Summary: "Reference for reusable workflow component fragments located under `workflows/components/`.",
		Example: "steps:\n  - id: write-config\n    kind: WriteFile\n    spec:\n      path: /etc/example.conf\n      content: hello\n  - id: restart-service\n    kind: ManageService\n    spec:\n      name: example\n      state: restarted\n",
		FieldDocs: map[string]FieldDoc{
			"steps": {Description: "Ordered list of workflow steps contained in this fragment.", Example: "[{id:example,kind:Command,spec:{...}}]"},
		},
		Notes: []string{
			"Component fragments are stored in the `workflows/components/` directory of your workspace.",
			"They contain only a `steps:` list and follow a restricted schema compared to full scenarios.",
			"Fragments are imported into a scenario phase using `phases[].imports`.",
			"For more details on where fragments fit in the project, see the [Workspace Layout](../../workspace-layout.md).",
		},
	}
}
