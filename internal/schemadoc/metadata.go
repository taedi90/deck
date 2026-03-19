package schemadoc

import (
	"maps"
	"sort"

	"github.com/taedi90/deck/internal/workflowcontract"
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
	MinimalExample string
	CuratedExample string
	ActionNotes    map[string]string
	ActionExamples map[string]string
	FieldDocs      map[string]FieldDoc
	Notes          []string
}

type PageMetadata struct {
	Title            string
	Summary          string
	MinimalExample   string
	RealisticExample string
	FieldDocs        map[string]FieldDoc
	Notes            []string
}

// commonFieldDocs describes the structural and execution-control fields shared by every step document.
// These are merged into every tool's FieldDocs so they appear consistently on all pages.
var commonFieldDocs = map[string]FieldDoc{
	"apiVersion": {Description: "Must be `deck/v1alpha1`.", Example: "deck/v1alpha1"},
	"id":         {Description: "Unique identifier for the step within the workflow. Used in logs and plan output.", Example: "configure-containerd"},
	"kind":       {Description: "Typed step kind. Determines which schema is applied to `spec`.", Example: "File"},
	"spec":       {Description: "Step-specific configuration payload. Shape depends on the chosen `kind`.", Example: "{...}"},
	"when":       {Description: workflowcontract.WhenDescription(), Example: workflowcontract.WhenExample()},
	"retry":      {Description: "Number of times to retry the step after a failure before marking it as failed.", Example: "3"},
	"timeout":    {Description: "Maximum duration allowed for the step before it is cancelled. Accepts Go duration strings.", Example: "5m"},
	"register":   {Description: workflowcontract.RegisterDescription(), Example: workflowcontract.RegisterExample()},
	"metadata":   {Description: "Optional free-form annotation map attached to the step for tooling or audit purposes.", Example: "{owner: platform-team}"},
}

var toolMetadata = map[string]ToolMetadata{
	"Artifacts": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-artifacts\nkind: Artifacts\nspec:\n" +
			"  artifacts:\n    - source:\n        amd64:\n          bundle:\n            root: files\n            path: bin/linux/amd64/example\n" +
			"        arm64:\n          bundle:\n            root: files\n            path: bin/linux/arm64/example\n      install:\n        path: /usr/local/bin/example\n",
		CuratedExample: "kind: Artifacts\nspec:\n  artifacts:\n    - source:\n        amd64:\n          bundle:\n            root: files\n" +
			"            path: bin/linux/amd64/runc\n        arm64:\n          bundle:\n            root: files\n            path: bin/linux/arm64/runc\n" +
			"      install:\n        path: /usr/local/sbin/runc\n        mode: \"0755\"\n",
		FieldDocs: map[string]FieldDoc{
			"spec.artifacts":                            {Description: "List of per-architecture artifact entries to install or extract.", Example: "[{source:{...},install:{...}}]"},
			"spec.artifacts[].source":                   {Description: "Per-architecture source references. Both `amd64` and `arm64` must be defined.", Example: "{amd64:{...},arm64:{...}}"},
			"spec.artifacts[].source.amd64":             {Description: "Source for amd64 nodes. Provide exactly one of `bundle`, `url`, or `path`.", Example: "{bundle:{root:files,path:bin/linux/amd64/runc}}"},
			"spec.artifacts[].source.amd64.bundle":      {Description: "Reference to a file inside the bundle prepared by `deck prepare`. Preferred for offline installs.", Example: "{root:files,path:bin/linux/amd64/runc}"},
			"spec.artifacts[].source.amd64.bundle.root": {Description: "Bundle root category to read from. Must match the output root used during prepare.", Example: "files"},
			"spec.artifacts[].source.amd64.bundle.path": {Description: "Path within the bundle root to the artifact file.", Example: "bin/linux/amd64/runc"},
			"spec.artifacts[].source.amd64.url":         {Description: "URL to download the artifact from. Used when a live network fetch is acceptable.", Example: "https://mirror.example.com/bin/runc"},
			"spec.artifacts[].source.amd64.path":        {Description: "Local filesystem path to the artifact. Used when the file is already present on the machine running prepare.", Example: "/opt/cache/runc"},
			"spec.artifacts[].source.amd64.sha256":      {Description: "Expected SHA-256 checksum of the artifact. Fails the step if the downloaded or copied file does not match.", Example: "abc123..."},
			"spec.artifacts[].source.arm64":             {Description: "Source for arm64 nodes. Provide exactly one of `bundle`, `url`, or `path`.", Example: "{bundle:{root:files,path:bin/linux/arm64/runc}}"},
			"spec.artifacts[].source.arm64.bundle":      {Description: "Reference to a file inside the bundle prepared by `deck prepare`. Preferred for offline installs.", Example: "{root:files,path:bin/linux/arm64/runc}"},
			"spec.artifacts[].source.arm64.bundle.root": {Description: "Bundle root category to read from. Must match the output root used during prepare.", Example: "files"},
			"spec.artifacts[].source.arm64.bundle.path": {Description: "Path within the bundle root to the artifact file.", Example: "bin/linux/arm64/runc"},
			"spec.artifacts[].source.arm64.url":         {Description: "URL to download the artifact from. Used when a live network fetch is acceptable.", Example: "https://mirror.example.com/bin/runc"},
			"spec.artifacts[].source.arm64.path":        {Description: "Local filesystem path to the artifact. Used when the file is already present on the machine running prepare.", Example: "/opt/cache/runc"},
			"spec.artifacts[].source.arm64.sha256":      {Description: "Expected SHA-256 checksum of the artifact. Fails the step if the downloaded or copied file does not match.", Example: "abc123..."},
			"spec.artifacts[].install":                  {Description: "Install the artifact as a single file at the given path. Use `install` or `extract`, not both.", Example: "{path:/usr/local/sbin/runc,mode:0755}"},
			"spec.artifacts[].install.path":             {Description: "Destination path on the node for the installed file.", Example: "/usr/local/sbin/runc"},
			"spec.artifacts[].install.mode":             {Description: "File permissions applied to the installed file in octal notation.", Example: "0755"},
			"spec.artifacts[].extract":                  {Description: "Extract the artifact archive into a directory. Use `install` or `extract`, not both.", Example: "{destination:/opt/cni/bin}"},
			"spec.artifacts[].extract.destination":      {Description: "Directory on the node where archive contents are extracted.", Example: "/opt/cni/bin"},
			"spec.artifacts[].extract.include":          {Description: "Optional list of paths to extract from the archive. Extracts all files when omitted.", Example: "[loopback,bridge]"},
			"spec.artifacts[].extract.mode":             {Description: "File permissions applied to extracted files in octal notation.", Example: "0755"},
			"spec.artifacts[].skipIfPresent":            {Description: "Skip this artifact entry if a file already exists at the given path.", Example: "{path:/usr/local/sbin/runc,executable:true}"},
			"spec.artifacts[].skipIfPresent.path":       {Description: "Filesystem path to check before installing or extracting.", Example: "/usr/local/sbin/runc"},
			"spec.artifacts[].skipIfPresent.executable": {Description: "Also assert that the existing file is executable. If false, the path check alone determines whether to skip.", Example: "true"},
			"spec.fetch":             {Description: "Shared fetch configuration applied as defaults across all artifact entries.", Example: "{offlineOnly:true}"},
			"spec.fetch.offlineOnly": {Description: "When true, reject any source that requires a live network fetch. Fails fast on entries without a bundle or local path source.", Example: "true"},
			"spec.fetch.strategy":    {Description: "Source selection strategy when multiple sources are defined per entry. `fallback` tries each source in order.", Example: "fallback"},
			"spec.fetch.sources":     {Description: "Ordered list of fallback fetch source configurations used when per-entry sources are not sufficient.", Example: "[{type:bundle}]"},
		},
		Notes: []string{
			"Each artifact entry must define both `source.amd64` and `source.arm64` so the bundle stays explicit across architectures.",
			"Each artifact entry must choose exactly one of `install` or `extract`.",
			"Use `skipIfPresent` to make the step idempotent when the binary may already exist on the node.",
		},
	},

	"Command": {
		CuratedExample: "kind: Command\nspec:\n  command: [\"systemctl\", \"status\", \"containerd\"]\n  timeout: 30s\n",
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

	"Containerd": {
		CuratedExample: "kind: Containerd\nspec:\n  path: /etc/containerd/config.toml\n  systemdCgroup: true\n  registryHosts:\n    - registry: registry.k8s.io\n      server: https://registry.k8s.io\n      host: http://registry.local:5000\n      capabilities: [pull, resolve]\n      skipVerify: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":                         {Description: "Destination path for the generated `config.toml`. Defaults to `/etc/containerd/config.toml`.", Example: "/etc/containerd/config.toml"},
			"spec.configPath":                   {Description: "Directory for per-registry `hosts.toml` files, used by containerd's registry host configuration model.", Example: "/etc/containerd/certs.d"},
			"spec.systemdCgroup":                {Description: "Enable systemd cgroup driver in the generated config. Required for Kubernetes nodes managed by systemd.", Example: "true"},
			"spec.createDefault":                {Description: "Write a minimal default config when no explicit config exists. Defaults to `true`.", Example: "true"},
			"spec.registryHosts":                {Description: "Per-registry host entries written as `hosts.toml` files under `configPath`. Each entry redirects a registry to a local mirror.", Example: "[{registry:registry.k8s.io,host:http://mirror.local:5000}]"},
			"spec.registryHosts[].registry":     {Description: "Registry namespace whose `hosts.toml` file will be written under `configPath`.", Example: "registry.k8s.io"},
			"spec.registryHosts[].server":       {Description: "Upstream registry server URL recorded in the generated `hosts.toml` file.", Example: "https://registry.k8s.io"},
			"spec.registryHosts[].host":         {Description: "Mirror host URL that containerd should contact for pulls and resolves.", Example: "http://registry.local:5000"},
			"spec.registryHosts[].capabilities": {Description: "Capabilities granted to the mirror host. Typical values are `pull` and `resolve`.", Example: "[pull,resolve]"},
			"spec.registryHosts[].skipVerify":   {Description: "Skip TLS verification for the mirror host. Useful for local mirrors with self-signed certificates.", Example: "true"},
		},
		Notes: []string{
			"Set `systemdCgroup: true` on all Kubernetes nodes to match the kubelet cgroup driver.",
			"Use `registryHosts` to point the runtime at an internal mirror instead of the public registry.",
		},
	},

	"Directory": {
		CuratedExample: "kind: Directory\nspec:\n  path: /home/vagrant/.kube\n  mode: \"0755\"\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path": {Description: "Directory path to create if it does not already exist. Parent directories are created as needed.", Example: "/var/lib/deck"},
			"spec.mode": {Description: "Directory permissions in octal notation. Applied after ensuring the directory exists, including on existing directories.", Example: "0755"},
		},
	},

	"File": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-file\nkind: File\nspec:\n  action: download\n  source:\n    url: https://example.invalid/file.tar.gz\n  output:\n    path: files/example.tar.gz\n",
		CuratedExample: "kind: File\nspec:\n  action: copy\n  src: /etc/kubernetes/admin.conf\n  dest: /home/vagrant/.kube/config\n  mode: \"0644\"\n",
		ActionNotes: map[string]string{
			"download": "Use `download` to pull or bundle a source into a staged output target. This is most common in prepare, but apply can also stage bundle outputs for later steps.",
			"write":    "Use `write` to write inline content or a rendered template to a destination path on the node.",
			"copy":     "Use `copy` to move a file already present on the node from one path to another.",
			"edit":     "Use `edit` for in-place match/replace edits on an existing file.",
		},
		ActionExamples: map[string]string{
			"download": "kind: File\nspec:\n  action: download\n  source:\n    bundle:\n      root: files\n      path: upstream/runc\n  output:\n    path: files/bin/runc\n",
			"write":    "kind: File\nspec:\n  action: write\n  path: /etc/containerd/config.toml\n  contentFromTemplate: |\n    [plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runc.options]\n      SystemdCgroup = {{ .vars.systemdCgroup }}\n  mode: \"0644\"\n",
			"copy":     "kind: File\nspec:\n  action: copy\n  src: /etc/kubernetes/admin.conf\n  dest: /home/vagrant/.kube/config\n  mode: \"0644\"\n",
			"edit":     "kind: File\nspec:\n  action: edit\n  path: /etc/containerd/config.toml\n  edits:\n    - match: SystemdCgroup = false\n      with: SystemdCgroup = true\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.action":              {Description: "Selects the file operation. Each action changes which sibling fields are required.", Example: "copy"},
			"spec.path":                {Description: "Destination path on the node. Required for `write` and `edit`.", Example: "/etc/containerd/config.toml"},
			"spec.content":             {Description: "Inline file content written verbatim to `path`. Used with `write`.", Example: "[offline-base]\\nbaseurl=http://repo.local"},
			"spec.contentFromTemplate": {Description: "Inline multi-line content rendered with the current vars before writing. Use this instead of `content` when the body includes template expressions such as `{{ .vars.* }}`.", Example: "[Service]\\nEnvironment=ROLE={{ .vars.role }}"},
			"spec.mode":                {Description: "File permissions in octal notation applied after `write`, `copy`, or `edit` actions complete.", Example: "0644"},
			"spec.backup":              {Description: "Create a `.bak` copy of the original file before overwriting it.", Example: "true"},
			"spec.src":                 {Description: "Source path already present on the node. Required for `copy`.", Example: "/etc/kubernetes/admin.conf"},
			"spec.dest":                {Description: "Destination path on the node. Required for `copy`.", Example: "/home/vagrant/.kube/config"},
			"spec.source":              {Description: "Download source descriptor. `path` or `bundle` may be combined with `url` to allow an online fallback when local resolution fails.", Example: "{url:https://example.invalid/file.tar.gz}"},
			"spec.source.url":          {Description: "URL to fetch the file from during prepare.", Example: "https://mirror.example.com/runc"},
			"spec.source.path":         {Description: "Local filesystem path to use as the source during prepare.", Example: "/opt/cache/runc"},
			"spec.source.sha256":       {Description: "Expected SHA-256 checksum. Fails the step if the fetched file does not match.", Example: "abc123..."},
			"spec.source.bundle":       {Description: "Reference to a file already inside the bundle. Used to stage a bundle-resident file into a new output location.", Example: "{root:files,path:bin/linux/amd64/runc}"},
			"spec.source.bundle.root":  {Description: "Bundle root category to read from (`files`, `images`, or `packages`).", Example: "files"},
			"spec.source.bundle.path":  {Description: "Relative path within the bundle root to the source file.", Example: "bin/linux/amd64/runc"},
			"spec.output":              {Description: "Optional output target inside the bundle for the downloaded file. When omitted, deck writes to `files/<basename>`.", Example: "{path:files/bin/runc}"},
			"spec.output.path":         {Description: "Bundle-relative path where the downloaded file is written. Defaults to `files/<basename>` when omitted.", Example: "files/bin/runc"},
			"spec.output.chmod":        {Description: "File permissions applied to the downloaded output file in octal notation.", Example: "0755"},
			"spec.edits":               {Description: "Ordered list of match/replace rules applied sequentially to the file. Required for `edit`.", Example: "[{match:SystemdCgroup = false,with:SystemdCgroup = true}]"},
			"spec.edits[].match":       {Description: "Literal string or pattern to search for in the file.", Example: "SystemdCgroup = false"},
			"spec.edits[].with":        {Description: "Replacement string. Substituted wherever `match` is found.", Example: "SystemdCgroup = true"},
			"spec.edits[].op":          {Description: "Edit operation type. `replace` substitutes all matches; `append` keeps the match text and adds `with` after each match. Defaults to `replace`.", Example: "replace"},
			"spec.fetch":               {Description: "Optional download transport settings applied to `download` fetches.", Example: "{offlineOnly:true}"},
		},
		Notes: []string{
			"`File` is usually the best first choice for host file changes because it stays declarative and validates action-specific inputs.",
			"`download` writes into a bundle output target during prepare, while `copy`, `write`, and `edit` operate on live node paths during apply.",
			"Use `contentFromTemplate` instead of `content` when the body includes variable substitution.",
		},
	},

	"Image": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-image\nkind: Image\nspec:\n  action: verify\n  images:\n    - registry.k8s.io/pause:3.9\n",
		CuratedExample: "kind: Image\nspec:\n  action: verify\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n    - registry.k8s.io/kube-controller-manager:v1.30.1\n",
		ActionNotes: map[string]string{
			"download": "Use `download` during prepare to collect images into bundle outputs.",
			"verify":   "Use `verify` to assert that required images already exist locally on the node.",
		},
		ActionExamples: map[string]string{
			"download": "kind: Image\nspec:\n  action: download\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n    - registry.example.com/platform/pause:3.9\n  auth:\n    - registry: registry.example.com\n      basic:\n        username: \"{{ .vars.registryUser }}\"\n        password: \"{{ .vars.registryPassword }}\"\n  output:\n    dir: images/control-plane\n",
			"verify":   "kind: Image\nspec:\n  action: verify\n  command: [ctr, -n, k8s.io, images, list, -q]\n  images:\n    - registry.k8s.io/kube-apiserver:v1.30.1\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.action":                {Description: "Chooses whether the step downloads images into the bundle or verifies their presence on the node.", Example: "verify"},
			"spec.images":                {Description: "Fully qualified image references to download or verify.", Example: "[registry.k8s.io/pause:3.9]"},
			"spec.auth":                  {Description: "Optional registry authentication entries for `download`. Match each private registry with credentials while leaving public registries to the default keychain.", Example: "[{registry:registry.example.com,basic:{username:robot,password:${REGISTRY_PASSWORD}}}]"},
			"spec.auth[].registry":       {Description: "Registry host matched against each image reference, for example `registry.example.com` or `ghcr.io`.", Example: "registry.example.com"},
			"spec.auth[].basic":          {Description: "Explicit basic-auth credentials used when downloading from the matched registry.", Example: "{username:robot,password:${REGISTRY_PASSWORD}}"},
			"spec.auth[].basic.username": {Description: "Registry username used for basic authentication.", Example: "robot"},
			"spec.auth[].basic.password": {Description: "Registry password or access token paired with `basic.username`.", Example: "${REGISTRY_PASSWORD}"},
			"spec.command":               {Description: "Optional image-listing command used by `verify` when the default runtime command is not appropriate.", Example: "[ctr,-n,k8s.io,images,list,-q]"},
			"spec.output":                {Description: "Bundle output settings for `download`. Deck writes one tar archive per image under `output.dir`.", Example: "{dir:images/control-plane}"},
			"spec.output.dir":            {Description: "Bundle-relative directory where per-image tar archives are written. Defaults to `images` when omitted.", Example: "images/control-plane"},
			"spec.backend":               {Description: "Backend-specific download settings such as image transfer engine configuration. Applies to `download` only.", Example: "{engine:go-containerregistry}"},
			"spec.backend.engine":        {Description: "Image download engine. Currently only `go-containerregistry` is supported.", Example: "go-containerregistry"},
		},
		Notes: []string{
			"Prefer `Image` over ad-hoc shell commands so workflows keep an explicit list of required images.",
			"Use explicit image tags or digests to keep prepared bundles reproducible.",
			"`spec.auth` is optional and only applies to `download`; when omitted, deck falls back to the environment's default registry keychain.",
		},
	},

	"Checks": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-checks\nkind: Checks\nspec:\n  checks: [os]\n",
		CuratedExample: "kind: Checks\nspec:\n  checks: [os, arch, swap]\n  failFast: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.checks":   {Description: "Named checks to run. Supported values include `os`, `arch`, `swap`, `kernelModules`, and `binaries`.", Example: "[os,arch,swap]"},
			"spec.binaries": {Description: "Binary names to verify are present in `PATH`. Used when `checks` includes `binaries`.", Example: "[kubeadm,kubelet,kubectl]"},
			"spec.failFast": {Description: "Stop on the first failing check rather than running all checks. Defaults to `true`.", Example: "true"},
		},
	},

	"KernelModule": {
		CuratedExample: "kind: KernelModule\nspec:\n  name: br_netfilter\n  load: true\n  persist: true\n  persistFile: /etc/modules-load.d/k8s.conf\n",
		FieldDocs: map[string]FieldDoc{
			"spec.name":        {Description: "Single module name to load. Use `name` or `names`, not both.", Example: "br_netfilter"},
			"spec.names":       {Description: "Multiple module names to load in a single step. Use `name` or `names`, not both.", Example: "[overlay,br_netfilter]"},
			"spec.load":        {Description: "Run `modprobe` to load the module immediately. Defaults to `true`.", Example: "true"},
			"spec.persist":     {Description: "Write the module name to a file under `/etc/modules-load.d/` so it is loaded on every boot. Defaults to `true`.", Example: "true"},
			"spec.persistFile": {Description: "Path to the persistence file written when `persist` is true.", Example: "/etc/modules-load.d/k8s.conf"},
		},
	},

	"Kubeadm": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-kubeadm\nkind: Kubeadm\nspec:\n  action: init\n  outputJoinFile: /tmp/deck/join.txt\n",
		CuratedExample: "kind: Kubeadm\nspec:\n  action: init\n  outputJoinFile: /tmp/deck/join.txt\n  podNetworkCIDR: 10.244.0.0/16\n  criSocket: unix:///run/containerd/containerd.sock\n  ignorePreflightErrors: [Swap]\n",
		ActionNotes: map[string]string{
			"init":  "`init` bootstraps a new control plane and writes a join artifact for worker nodes.",
			"join":  "`join` consumes either a prepared join command file or a kubeadm JoinConfiguration file and adds the node to an existing cluster.",
			"reset": "`reset` tears down kubeadm-managed state before rebuilding or reprovisioning.",
		},
		ActionExamples: map[string]string{
			"init":  "kind: Kubeadm\nspec:\n  action: init\n  outputJoinFile: /tmp/deck/join.txt\n  podNetworkCIDR: 10.244.0.0/16\n",
			"join":  "kind: Kubeadm\nspec:\n  action: join\n  configFile: /tmp/deck/kubeadm-join.yaml\n  extraArgs: [--skip-phases=preflight]\n",
			"reset": "kind: Kubeadm\nspec:\n  action: reset\n  force: true\n  removePaths: [/etc/cni/net.d, /var/lib/etcd]\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.action":                {Description: "Selects the kubeadm subcommand to run: `init`, `join`, or `reset`.", Example: "init"},
			"spec.outputJoinFile":        {Description: "Path where the generated join command is written after `init`. Worker nodes read this file to join the cluster.", Example: "/tmp/deck/join.txt"},
			"spec.joinFile":              {Description: "Path to the join command file produced by a prior `init` run. For `join`, provide this or `configFile`.", Example: "/tmp/deck/join.txt"},
			"spec.configFile":            {Description: "Path to an explicit kubeadm config file passed with `--config`. For `join`, provide this or `joinFile`. For `init`, combine it with `configTemplate` or a pre-rendered kubeadm config.", Example: "/tmp/deck/kubeadm.conf"},
			"spec.configTemplate":        {Description: "For `init`, use `default` for the deck-managed kubeadm config template. Any other non-empty value is written literally as inline kubeadm YAML content to `configFile`.", Example: "default"},
			"spec.kubernetesVersion":     {Description: "Kubernetes version string passed to kubeadm. Accepts the `{{ .vars.* }}` template syntax.", Example: "v1.30.1"},
			"spec.advertiseAddress":      {Description: "API server advertise address for `init`. Use `auto` to detect the primary interface, or provide an explicit IP.", Example: "auto"},
			"spec.podNetworkCIDR":        {Description: "CIDR range for the pod network passed to `init`. Must not overlap with node or service CIDRs.", Example: "10.244.0.0/16"},
			"spec.criSocket":             {Description: "CRI socket path passed to kubeadm. Required when multiple container runtimes are installed on the node.", Example: "unix:///run/containerd/containerd.sock"},
			"spec.pullImages":            {Description: "Pull required control-plane images before running `kubeadm init`. Requires network or a pre-configured mirror.", Example: "true"},
			"spec.ignorePreflightErrors": {Description: "Kubeadm preflight check names to suppress. Use sparingly and only for known-safe deviations.", Example: "[Swap]"},
			"spec.extraArgs":             {Description: "Additional flags passed directly to the kubeadm subcommand as `--key=value` pairs.", Example: "[--skip-phases=addon/kube-proxy]"},
			"spec.skipIfAdminConfExists": {Description: "Skip the `init` step if `/etc/kubernetes/admin.conf` already exists, treating the node as already bootstrapped. Defaults to `true`.", Example: "true"},
			"spec.asControlPlane":        {Description: "When `true`, adds `--control-plane` so the node joins as an additional control-plane member rather than a worker.", Example: "false"},
			"spec.force":                 {Description: "Pass `--force` to `kubeadm reset` to skip interactive confirmation prompts.", Example: "true"},
			"spec.ignoreErrors":          {Description: "For `reset`, continue with filesystem and runtime cleanup even if the `kubeadm reset` command itself fails. Later cleanup steps still fail the step if they error.", Example: "true"},
			"spec.stopKubelet":           {Description: "Stop the kubelet service before running `reset`. Defaults to `true`.", Example: "true"},
			"spec.removePaths":           {Description: "Directories to delete during `reset` cleanup, such as CNI and etcd data.", Example: "[/etc/cni/net.d,/var/lib/etcd]"},
			"spec.removeFiles":           {Description: "Individual files to delete during `reset` cleanup, such as kubeconfig files.", Example: "[/etc/kubernetes/admin.conf]"},
			"spec.cleanupContainers":     {Description: "Container names to stop and remove during `reset`. Useful when the runtime has stale control-plane containers.", Example: "[kube-apiserver,etcd]"},
			"spec.restartRuntimeService": {Description: "Container runtime service name to restart after `reset` cleanup completes.", Example: "containerd"},
		},
		Notes: []string{
			"The action controls the contract: `init` requires `outputJoinFile`, `join` requires exactly one of `joinFile` or `configFile`, and `reset` focuses on cleanup fields.",
			"Kubeadm fields are action-scoped: validation rejects `join`-only fields on `init`, `init`-only fields on `reset`, and other cross-action mixes.",
			"When `skipIfAdminConfExists` skips `init`, deck does not create a new join artifact and registered `joinFile` outputs are unavailable unless the file already exists.",
			"Place host preparation steps (`Containerd`, `Swap`, `KernelModule`, `Sysctl`) before `Kubeadm` so bootstrap failures point to the correct step.",
		},
	},

	"PackageCache": {
		CuratedExample: "kind: PackageCache\nspec:\n  manager: apt\n  clean: true\n  update: true\n  restrictToRepos:\n    - /etc/apt/sources.list.d/offline.list\n",
		FieldDocs: map[string]FieldDoc{
			"spec.manager":         {Description: "Package manager to use. `auto` detects from the host OS. Supports `apt` and `dnf`.", Example: "apt"},
			"spec.clean":           {Description: "Run a cache clean before updating metadata (`apt clean` / `dnf clean all`).", Example: "true"},
			"spec.update":          {Description: "Fetch fresh package metadata from the configured repositories (`apt update` / `dnf makecache`).", Example: "true"},
			"spec.restrictToRepos": {Description: "Limit the metadata update to these repository selectors. For apt, use repo file paths or globs; for dnf, use repo IDs. Prevents fetching from online repos during an offline install.", Example: "[/etc/apt/sources.list.d/offline.list]"},
			"spec.excludeRepos":    {Description: "Repository selectors to skip during metadata update. For apt, selectors match repo file paths; for dnf, they match repo IDs.", Example: "[updates]"},
		},
	},

	"Packages": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-packages\nkind: Packages\nspec:\n  action: install\n  packages: [kubelet]\n",
		CuratedExample: "kind: Packages\nspec:\n  action: install\n  packages: [kubelet, kubeadm, kubectl]\n",
		ActionNotes: map[string]string{
			"download": "`download` resolves and gathers packages into a prepare artifact set without installing them.",
			"install":  "`install` applies packages on the node using the available package manager.",
		},
		ActionExamples: map[string]string{
			"download": "kind: Packages\nspec:\n  action: download\n  packages: [podman]\n  distro:\n    family: rhel\n    release: rocky9\n  repo:\n    type: yum\n    modules:\n      - name: container-tools\n        stream: \"4.0\"\n  backend:\n    mode: container\n    runtime: docker\n    image: rockylinux:9\n",
			"install":  "kind: Packages\nspec:\n  action: install\n  packages: [kubelet, kubeadm, kubectl]\n  source:\n    type: local-repo\n    path: /opt/deck/repos/kubernetes\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.action":                {Description: "Selects whether to collect packages during prepare (`download`) or install them on the node (`install`).", Example: "install"},
			"spec.packages":              {Description: "Package names to download or install. Use the same list in both `download` and `install` steps to keep offline parity.", Example: "[kubelet,kubeadm,kubectl]"},
			"spec.source":                {Description: "Local repository source for `install`. Points to a pre-prepared on-disk package repo instead of relying on configured package manager sources.", Example: "{type:local-repo,path:/opt/deck/repos/kubernetes}"},
			"spec.source.type":           {Description: "Source type. Currently `local-repo` is the only supported value.", Example: "local-repo"},
			"spec.source.path":           {Description: "Filesystem path to the pre-prepared local package repository.", Example: "/opt/deck/repos/kubernetes"},
			"spec.restrictToRepos":       {Description: "For `install`, limit package manager visibility to these repository selectors. For apt, use repo file paths or globs; for dnf, use repo IDs. Prevents accidental pulls from other configured repos.", Example: "[offline-kubernetes]"},
			"spec.excludeRepos":          {Description: "For `install`, repository selectors to exclude from package resolution. For apt, selectors match repo file paths; for dnf, they match repo IDs.", Example: "[updates]"},
			"spec.distro":                {Description: "Target distribution hint used by `download` to select the correct package manager and resolver backend.", Example: "{family:rhel,release:rocky9}"},
			"spec.repo":                  {Description: "Package-manager repository settings applied before `download`, including repo layout generation and RPM module streams.", Example: "{type:yum,modules:[...]}"},
			"spec.repo.type":             {Description: "Repository output type for `download` repo mode. Supported values are `apt-flat` and `yum`.", Example: "yum"},
			"spec.repo.generate":         {Description: "When `true`, generate repository metadata after the package payload is collected. Used with `repo.type` in download repo mode.", Example: "true"},
			"spec.repo.pkgsDir":          {Description: "Subdirectory under the generated repo root where package payloads are written. Defaults to `pkgs`.", Example: "pkgs"},
			"spec.repo.modules":          {Description: "RPM module streams to enable before resolving downloads on RHEL-family systems.", Example: "[{name:container-tools,stream:4.0}]"},
			"spec.repo.modules[].name":   {Description: "RPM module name to enable.", Example: "container-tools"},
			"spec.repo.modules[].stream": {Description: "Module stream version paired with the module name.", Example: "4.0"},
			"spec.backend":               {Description: "Container-based download backend for `download`. When provided, `backend.mode=container` and `backend.image` are required.", Example: "{mode:container,runtime:docker,image:rockylinux:9}"},
			"spec.backend.mode":          {Description: "Download backend mode. Currently only `container` is supported.", Example: "container"},
			"spec.backend.runtime":       {Description: "Preferred container runtime for the download helper container. Supported values are `docker`, `podman`, or `auto`.", Example: "docker"},
			"spec.backend.image":         {Description: "Container image used for package resolution in `download` mode. Required when `backend` is set.", Example: "rockylinux:9"},
			"spec.output":                {Description: "Optional bundle output settings for `download`. When set, `output.dir` changes the directory where downloaded package artifacts are written.", Example: "{dir:packages/kubernetes}"},
			"spec.output.dir":            {Description: "Bundle-relative directory used by `download` for downloaded package artifacts. Defaults to `packages` or a repo-derived path when omitted.", Example: "packages/kubernetes"},
		},
		Notes: []string{
			"Use `Packages` with `Repository` and `PackageCache` for a complete typed package-management flow.",
			"Keeping the same package list across `download` and `install` helps maintain offline parity.",
			"Use `restrictToRepos` on the `install` step to prevent the node's default online repos from being consulted during an offline apply.",
			"When `repo` is set for `download`, deck expects `repo.type` and `distro.release` so it can build an apt-flat or yum-style repository layout.",
			"Without a container download backend, `download` currently writes placeholder package markers instead of resolving real packages.",
		},
	},

	"Repository": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-repository\nkind: Repository\nspec:\n  action: configure\n  format: apt\n  repositories:\n    - id: offline\n      baseurl: http://repo.local/debian\n",
		CuratedExample: "kind: Repository\nspec:\n  action: configure\n  format: apt\n  replaceExisting: true\n  refreshCache:\n    clean: true\n    update: true\n  repositories:\n    - id: offline\n      baseurl: http://repo.local/debian\n      trusted: true\n",
		ActionNotes: map[string]string{
			"configure": "`configure` writes or rewrites repository definition files and optionally triggers a cache refresh.",
		},
		ActionExamples: map[string]string{
			"configure": "kind: Repository\nspec:\n  action: configure\n  format: apt\n  path: /etc/apt/sources.list.d/offline.list\n  repositories:\n    - id: offline\n      baseurl: http://repo.local/debian\n      trusted: true\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.action":               {Description: "Operation to perform. Currently only `configure` is supported.", Example: "configure"},
			"spec.format":               {Description: "Repository file format to write. `auto` detects from the host family, `apt` produces a sources.list entry, and `yum` produces a `.repo` file.", Example: "apt"},
			"spec.path":                 {Description: "Explicit output path for the generated repository file. Defaults to `/etc/apt/sources.list.d/deck-offline.list` for apt or `/etc/yum.repos.d/deck-offline.repo` for yum when omitted.", Example: "/etc/apt/sources.list.d/offline.list"},
			"spec.mode":                 {Description: "File permissions applied to the generated repository file in octal notation.", Example: "0644"},
			"spec.replaceExisting":      {Description: "Replace an existing repository file at the target path before writing the new definition.", Example: "true"},
			"spec.disableExisting":      {Description: "Disable all existing repository definitions before writing the new one. Prevents conflicts from online repos during offline installs.", Example: "true"},
			"spec.backupPaths":          {Description: "Paths to back up before modifying. Backed-up files are saved with a `.bak` suffix.", Example: "[/etc/apt/sources.list]"},
			"spec.cleanupPaths":         {Description: "Paths to remove before writing the new repository definition.", Example: "[/etc/apt/sources.list.d/ubuntu.list]"},
			"spec.refreshCache":         {Description: "Optional package metadata refresh that runs after repository files are written. The block is enabled by default when present, and behaves like a follow-up `PackageCache` step.", Example: "{clean:true,update:true}"},
			"spec.refreshCache.enabled": {Description: "Whether the refresh block should run. Defaults to `true` when `refreshCache` is present.", Example: "true"},
			"spec.refreshCache.clean":   {Description: "Run the package-manager cache clean command before refreshing metadata.", Example: "true"},
			"spec.refreshCache.update":  {Description: "Run the package-manager metadata update command after writing repo files. Defaults to `true` when omitted.", Example: "true"},
			"spec.repositories":         {Description: "Repository entries to write. Each entry maps to one repository block in the generated file.", Example: "[{id:offline,baseurl:http://repo.local/debian}]"},
		},
		Notes: []string{
			"`Repository` only writes repository definition files. Combine it with `PackageCache` when the package manager needs an explicit metadata refresh.",
			"Keep repository definitions mirror-specific rather than mutating the host's default online sources.",
		},
	},

	"Service": {
		CuratedExample: "kind: Service\nspec:\n  name: containerd\n  enabled: true\n  state: started\n",
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
		CuratedExample: "kind: Swap\nspec:\n  disable: true\n  persist: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.disable":   {Description: "Disable all active swap devices with `swapoff -a`. Defaults to `true`.", Example: "true"},
			"spec.persist":   {Description: "Comment out swap entries in `/etc/fstab` so swap stays off after reboot. Defaults to `true`.", Example: "true"},
			"spec.fstabPath": {Description: "Path to the fstab file. Defaults to `/etc/fstab`.", Example: "/etc/fstab"},
		},
	},

	"Symlink": {
		CuratedExample: "kind: Symlink\nspec:\n  path: /usr/bin/runc\n  target: /usr/local/sbin/runc\n  force: true\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":          {Description: "Path where the symbolic link will be created.", Example: "/usr/bin/runc"},
			"spec.target":        {Description: "Path that the symbolic link points to.", Example: "/usr/local/sbin/runc"},
			"spec.force":         {Description: "Remove an existing file or link at `path` before creating the new link. Defaults to `false`.", Example: "true"},
			"spec.createParent":  {Description: "Create parent directories for `path` if they do not exist. Defaults to `false`.", Example: "true"},
			"spec.requireTarget": {Description: "Fail the step if `target` does not exist at the time the link is created. Defaults to `false`.", Example: "true"},
		},
	},

	"Sysctl": {
		CuratedExample: "kind: Sysctl\nspec:\n  writeFile: /etc/sysctl.d/99-kubernetes-cri.conf\n  apply: true\n  values:\n    net.ipv4.ip_forward: 1\n",
		FieldDocs: map[string]FieldDoc{
			"spec.writeFile": {Description: "Path to the sysctl file written with the given values. A drop-in under `/etc/sysctl.d/` is the common choice.", Example: "/etc/sysctl.d/99-k8s.conf"},
			"spec.values":    {Description: "Map of sysctl key-value pairs to write and optionally apply.", Example: "{net.ipv4.ip_forward:1,net.bridge.bridge-nf-call-iptables:1}"},
			"spec.apply":     {Description: "Run `sysctl -p <writeFile>` after writing the file to apply the values immediately without a reboot. Defaults to `false`.", Example: "true"},
		},
	},

	"SystemdUnit": {
		CuratedExample: "kind: SystemdUnit\nspec:\n  path: /etc/systemd/system/kubelet.service\n  contentFromTemplate: |\n    [Unit]\n    Description=Kubelet\n\n    [Service]\n    Environment=NODE_IP={{ .vars.nodeIP }}\n  daemonReload: true\n  service:\n    enabled: true\n    state: started\n",
		FieldDocs: map[string]FieldDoc{
			"spec.path":                {Description: "Destination path for the unit file on the node.", Example: "/etc/systemd/system/kubelet.service"},
			"spec.content":             {Description: "Inline unit file content written verbatim to `path`.", Example: "[Unit]\\nDescription=kubelet"},
			"spec.contentFromTemplate": {Description: "Inline multi-line unit content rendered with the current vars before writing. Prefer this for parameterized unit files.", Example: "[Service]\\nEnvironment=NODE_IP={{ .vars.nodeIP }}"},
			"spec.mode":                {Description: "File permissions applied to the unit file in octal notation.", Example: "0644"},
			"spec.daemonReload":        {Description: "Run `systemctl daemon-reload` after writing the unit file so systemd picks up the change.", Example: "true"},
			"spec.service":             {Description: "Optional service management block run after the unit file is written.", Example: "{name:kubelet,enabled:true,state:started}"},
			"spec.service.name":        {Description: "Service name to manage. Defaults to the unit file basename when omitted.", Example: "kubelet.service"},
			"spec.service.enabled":     {Description: "Whether the service should be enabled to start on boot.", Example: "true"},
			"spec.service.state":       {Description: "Desired service state after writing the unit file.", Example: "started"},
		},
	},

	"Wait": {
		MinimalExample: "apiVersion: deck/v1alpha1\nid: example-wait\nkind: Wait\nspec:\n  action: serviceActive\n  name: containerd\n",
		CuratedExample: "kind: Wait\nspec:\n  action: fileExists\n  path: /etc/kubernetes/admin.conf\n  type: file\n  nonEmpty: true\n  interval: 2s\n  timeout: 5m\n",
		ActionNotes: map[string]string{
			"serviceActive":  "Wait until a systemd service reports an active state.",
			"commandSuccess": "Poll a command until it exits with code 0.",
			"fileExists":     "Wait for a file or directory path to appear.",
			"fileAbsent":     "Wait for a file or directory path to disappear.",
			"tcpPortOpen":    "Wait for a TCP listener to accept connections on the given port.",
			"tcpPortClosed":  "Wait for a TCP port to stop accepting connections.",
		},
		ActionExamples: map[string]string{
			"serviceActive":  "kind: Wait\nspec:\n  action: serviceActive\n  name: containerd\n  interval: 2s\n  timeout: 2m\n",
			"commandSuccess": "kind: Wait\nspec:\n  action: commandSuccess\n  command: [test, -f, /etc/kubernetes/admin.conf]\n  interval: 2s\n  timeout: 2m\n",
			"fileExists":     "kind: Wait\nspec:\n  action: fileExists\n  path: /etc/kubernetes/admin.conf\n  type: file\n  nonEmpty: true\n  interval: 2s\n  timeout: 5m\n",
			"fileAbsent":     "kind: Wait\nspec:\n  action: fileAbsent\n  path: /var/lib/etcd/member\n  interval: 2s\n  timeout: 2m\n",
			"tcpPortOpen":    "kind: Wait\nspec:\n  action: tcpPortOpen\n  port: \"6443\"\n  interval: 2s\n  timeout: 5m\n",
			"tcpPortClosed":  "kind: Wait\nspec:\n  action: tcpPortClosed\n  port: \"10250\"\n  interval: 2s\n  timeout: 2m\n",
		},
		FieldDocs: map[string]FieldDoc{
			"spec.action":       {Description: "Selects the condition to poll: `serviceActive`, `commandSuccess`, `fileExists`, `fileAbsent`, `tcpPortOpen`, or `tcpPortClosed`.", Example: "fileExists"},
			"spec.name":         {Description: "Service name to check. Required for `serviceActive`.", Example: "containerd"},
			"spec.command":      {Description: "Command vector to run on each poll attempt. Required for `commandSuccess`. The step succeeds when the command exits 0.", Example: "[test,-f,/etc/kubernetes/admin.conf]"},
			"spec.path":         {Description: "Filesystem path to check. Required for `fileExists` and `fileAbsent`.", Example: "/etc/kubernetes/admin.conf"},
			"spec.type":         {Description: "Restricts the path check to a specific filesystem entry type. `file` matches regular files only, `dir` matches directories, `any` matches either. Defaults to `any`.", Example: "file"},
			"spec.nonEmpty":     {Description: "For `fileExists`, also assert that the file has non-zero size. Useful when waiting for a file that is written progressively.", Example: "true"},
			"spec.port":         {Description: "TCP port number to check. Required for `tcpPortOpen` and `tcpPortClosed`.", Example: "6443"},
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

func ToolMeta(kind string) ToolMetadata {
	def, hasDefinition := workflowcontract.StepDefinitionForKind(kind)
	meta, ok := toolMetadata[kind]
	if !ok && !hasDefinition {
		return ToolMetadata{Kind: kind, Category: "other", Summary: "Generated schema reference.", WhenToUse: "Use this schema according to the workflow contract."}
	}
	meta.Kind = kind
	if hasDefinition {
		meta.Category = def.Category
		meta.Summary = def.Summary
		meta.WhenToUse = def.WhenToUse
	}
	// Merge common field docs so every tool page documents shared execution controls.
	merged := make(map[string]FieldDoc, len(commonFieldDocs)+len(meta.FieldDocs))
	maps.Copy(merged, commonFieldDocs)
	maps.Copy(merged, meta.FieldDocs)
	meta.FieldDocs = merged
	return meta
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
		Title:            "Workflow Schema",
		Summary:          "Top-level workflow authoring reference for deck workflows.",
		MinimalExample:   "role: apply\nversion: v1alpha1\nsteps:\n  - id: write-config\n    apiVersion: deck/v1alpha1\n    kind: File\n    spec:\n      action: write\n      path: /etc/example.conf\n      content: hello\n",
		RealisticExample: "role: prepare\nversion: v1alpha1\nartifacts:\n  files:\n    - group: runtime-binaries\n      items:\n        - id: runc\n          source:\n            url: https://mirror.example.invalid/runc\n          output:\n            path: bin/runc\n",
		FieldDocs: map[string]FieldDoc{
			"role":                           {Description: "Workflow role. `prepare` builds offline artifacts; `apply` changes the local node.", Example: "apply"},
			"artifacts":                      {Description: "Declarative prepare inventory that replaces legacy prepare download steps.", Example: "{files:[...],images:[...],packages:[...]}"},
			"artifacts.files[].execution":    {Description: "Optional execution controls for file artifact jobs. `parallelism` bounds concurrent downloads and `retry` applies per file job.", Example: "{parallelism:4,retry:2}"},
			"artifacts.images[].execution":   {Description: "Optional execution controls for image artifact jobs. `parallelism` bounds concurrent image pulls and `retry` applies per image job.", Example: "{parallelism:3,retry:1}"},
			"artifacts.packages[].execution": {Description: "Optional execution controls for package artifact jobs. `parallelism` runs different target/container jobs concurrently and `retry` applies per target job.", Example: "{parallelism:2,retry:1}"},
			"phases":                         {Description: "Ordered execution phases. Each phase can contain imports, steps, or both.", Example: "[{name:install,steps:[...]}]"},
			"steps":                          {Description: "Flat step list for workflows that do not need named phases.", Example: "[{id:configure-runtime,kind:Containerd,spec:{...}}]"},
			"steps[].kind":                   {Description: "Typed step kind selected from the shipped public step inventory.", Example: "File"},
			"steps[].spec":                   {Description: "Action-specific step payload validated against the schema for the chosen kind.", Example: "{action:install,path:/etc/example.conf,content:hello}"},
			"steps[].when":                   {Description: workflowcontract.WhenDescription(), Example: `vars.skipSetup != "true"`},
			"steps[].retry":                  {Description: "Number of times to retry the step after a failure before marking it as failed.", Example: "3"},
			"steps[].timeout":                {Description: "Maximum duration for the step. Accepts Go duration strings.", Example: "5m"},
			"steps[].register":               {Description: workflowcontract.RegisterDescription(), Example: workflowcontract.RegisterExample()},
			"phases[].name":                  {Description: "Stable phase name used for ordering and selective execution.", Example: "install"},
			"phases[].imports":               {Description: "Component fragment imports that expand into this phase before step execution.", Example: "[{path:k8s/containerd-kubelet.yaml}]"},
		},
		Notes: []string{
			"A workflow must define at least one of `artifacts`, `phases`, or `steps`.",
			"A workflow cannot define both top-level `phases` and top-level `steps` at the same time.",
			"Imports are only supported under `phases[].imports` and resolve from `workflows/components/`.",
			"Artifact execution controls are opt-in and only affect `role: prepare` artifact jobs; they do not introduce general step-level parallelism.",
			"Each step still validates against its own kind-specific schema after the top-level workflow schema passes.",
		},
	}
}

func ToolDefinitionMeta() PageMetadata {
	return PageMetadata{
		Title:            "Tool Definition Schema",
		Summary:          "Reference for tool definition manifests used to describe typed workflow tools.",
		MinimalExample:   "apiVersion: deck/v1\nkind: ToolDefinition\nmetadata:\n  name: File\nspec:\n  version: v1\n  summary: Manage files\n  category: shared\n  inputSchema: {}\n",
		RealisticExample: "apiVersion: deck/v1\nkind: ToolDefinition\nmetadata:\n  name: File\n  description: Manage files on the local node\nspec:\n  version: v1\n  summary: Manage files on the local node\n  category: shared\n  offlineAllowed: true\n  requires:\n    root: true\n  inputSchema: {}\n  idempotency:\n    mode: by-output\n    keys: [path]\n",
		FieldDocs: map[string]FieldDoc{
			"metadata.name":                  {Description: "Public tool kind name described by this manifest.", Example: "File"},
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
		Title:            "Component Fragment Schema",
		Summary:          "Reference for reusable workflow component fragments located under `workflows/components/`.",
		MinimalExample:   "steps:\n  - id: example-step\n    kind: Command\n    spec:\n      command: [echo, hello]\n",
		RealisticExample: "steps:\n  - id: write-config\n    kind: File\n    spec:\n      action: write\n      path: /etc/example.conf\n      content: hello\n  - id: restart-service\n    kind: Service\n    spec:\n      name: example\n      state: restarted\n",
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
