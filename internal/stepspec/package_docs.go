package stepspec

var (
	commonRepositoryFieldDocs = map[string]FieldDoc{
		"spec.format":                   {Description: "Repository file format to write.", Example: "deb"},
		"spec.path":                     {Description: "Explicit output path for the generated repository file.", Example: "/etc/apt/sources.list.d/offline.list"},
		"spec.mode":                     {Description: "File permissions applied to the generated repository file in octal notation.", Example: "0644"},
		"spec.replaceExisting":          {Description: "Replace an existing repository file at the target path before writing the new definition.", Example: "true"},
		"spec.disableExisting":          {Description: "Disable all existing repository definitions before writing the new one.", Example: "true"},
		"spec.backupPaths":              {Description: "Paths to back up before modifying.", Example: "[/etc/apt/sources.list]"},
		"spec.cleanupPaths":             {Description: "Paths to remove before writing the new repository definition.", Example: "[/etc/apt/sources.list.d/ubuntu.list]"},
		"spec.repositories":             {Description: "Repository entries to write.", Example: "[{baseurl:http://repo.local/debian,trusted:true}]"},
		"spec.repositories[].id":        {Description: "RPM repository ID.", Example: "offline-kubernetes"},
		"spec.repositories[].name":      {Description: "Human-readable repository name.", Example: "Offline Kubernetes"},
		"spec.repositories[].baseurl":   {Description: "Base URL for the repository.", Example: "http://repo.local/debian"},
		"spec.repositories[].enabled":   {Description: "Explicit enabled state for the repository entry.", Example: "true"},
		"spec.repositories[].gpgcheck":  {Description: "Explicit gpgcheck state for RPM repositories.", Example: "true"},
		"spec.repositories[].gpgkey":    {Description: "URL or path to the repository GPG key.", Example: "file:///etc/pki/rpm-gpg/RPM-GPG-KEY-offline"},
		"spec.repositories[].trusted":   {Description: "Mark a deb repository as trusted.", Example: "true"},
		"spec.repositories[].suite":     {Description: "Deb repository suite.", Example: "stable"},
		"spec.repositories[].component": {Description: "Deb repository component.", Example: "main"},
		"spec.repositories[].type":      {Description: "Deb repository type.", Example: "deb"},
		"spec.repositories[].extra":     {Description: "Additional rpm-style repository key-value pairs.", Example: "{priority:10,module_hotfixes:true}"},
	}

	_ = registerToolDoc("ConfigureRepository", ToolDocMetadata{
		Example:   "kind: ConfigureRepository\nspec:\n  format: deb\n  path: /etc/apt/sources.list.d/offline.list\n  repositories:\n    - baseurl: http://repo.local/debian\n      trusted: true\n",
		FieldDocs: commonRepositoryFieldDocs,
		Notes: []string{
			"`ConfigureRepository` only writes repository definition files.",
			"Use `RefreshRepository` when the package manager needs an explicit metadata refresh.",
			"Keep repository definitions mirror-specific rather than mutating the host's default online sources.",
		},
	})

	_ = registerToolDoc("RefreshRepository", ToolDocMetadata{
		Example: "kind: RefreshRepository\nspec:\n  manager: apt\n  clean: true\n  update: true\n  restrictToRepos:\n    - /etc/apt/sources.list.d/offline.list\n",
		FieldDocs: map[string]FieldDoc{
			"spec.manager":         {Description: "Package manager to use for repository metadata refresh.", Example: "apt"},
			"spec.clean":           {Description: "Run a cache clean before updating metadata.", Example: "true"},
			"spec.update":          {Description: "Fetch fresh package metadata from the configured repositories.", Example: "true"},
			"spec.restrictToRepos": {Description: "Limit the metadata update to these repository selectors.", Example: "[/etc/apt/sources.list.d/offline.list]"},
			"spec.excludeRepos":    {Description: "Repository selectors to skip during metadata update.", Example: "[updates]"},
			"spec.timeout":         {Description: "Maximum total duration for refresh operations.", Example: "5m"},
		},
		Notes: []string{"Use `RefreshRepository` after writing repo definitions and before package installs that depend on fresh metadata."},
	})

	_ = registerToolDoc("DownloadPackage", ToolDocMetadata{
		Example: "kind: DownloadPackage\nspec:\n  packages: [podman]\n  distro:\n    family: rhel\n    release: rocky9\n  repo:\n    type: rpm\n    modules:\n      - name: container-tools\n        stream: \"4.0\"\n  backend:\n    mode: container\n    runtime: docker\n    image: rockylinux:9\n",
		FieldDocs: map[string]FieldDoc{
			"spec.packages":              {Description: "Package names to download.", Example: "[kubelet,kubeadm,kubectl]"},
			"spec.distro":                {Description: "Target distribution hint used to select resolver behavior.", Example: "{family:rhel,release:rocky9}"},
			"spec.distro.family":         {Description: "Distribution family used to resolve package tooling.", Example: "rhel"},
			"spec.distro.release":        {Description: "Distribution release used for resolver and repo layout selection.", Example: "rocky9"},
			"spec.repo":                  {Description: "Repository settings applied before download.", Example: "{type:rpm,modules:[{name:container-tools,stream:4.0}]}"},
			"spec.repo.type":             {Description: "Repository output type for download repo mode.", Example: "rpm"},
			"spec.repo.generate":         {Description: "Generate repository metadata after collecting packages.", Example: "true"},
			"spec.repo.pkgsDir":          {Description: "Subdirectory under the generated repo root where packages are written.", Example: "pkgs"},
			"spec.repo.modules":          {Description: "RPM module streams to enable before resolving downloads.", Example: "[{name:container-tools,stream:4.0}]"},
			"spec.repo.modules[].name":   {Description: "RPM module name to enable.", Example: "container-tools"},
			"spec.repo.modules[].stream": {Description: "Module stream version paired with the module name.", Example: "4.0"},
			"spec.backend":               {Description: "Container-based download backend configuration.", Example: "{mode:container,runtime:docker,image:rockylinux:9}"},
			"spec.backend.mode":          {Description: "Download backend mode.", Example: "container"},
			"spec.backend.runtime":       {Description: "Preferred container runtime for the download helper container.", Example: "docker"},
			"spec.backend.image":         {Description: "Container image used for package resolution in download mode.", Example: "rockylinux:9"},
			"spec.outputDir":             {Description: "Optional bundle-relative output directory for downloaded package artifacts.", Example: "packages/kubernetes"},
			"spec.timeout":               {Description: "Maximum total duration for package download operations.", Example: "30m"},
		},
		Notes: []string{
			"Use `DownloadPackage` during prepare to stage offline package-manager content.",
			"Omit `outputDir` unless you need a custom package location.",
			"Container-backed `DownloadPackage` exports completed artifacts into a host-owned cache and does not bind-mount package-manager cache directories.",
			"Keeping the same package list across `download` and `install` helps maintain offline parity.",
			"Use `restrictToRepos` on the `InstallPackage` step to prevent the node's default online repos from being consulted during an offline apply.",
			"When `repo` is set for `DownloadPackage`, deck expects `repo.type` and `distro.release` so it can build a `deb-flat` or `rpm` repository layout.",
		},
	})

	_ = registerToolDoc("InstallPackage", ToolDocMetadata{
		Example: "kind: InstallPackage\nspec:\n  packages: [kubelet, kubeadm, kubectl]\n  source:\n    type: local-repo\n    path: /opt/deck/repos/kubernetes\n",
		FieldDocs: map[string]FieldDoc{
			"spec.packages":        {Description: "Package names to install.", Example: "[kubelet,kubeadm,kubectl]"},
			"spec.source":          {Description: "Local repository source used for the installation.", Example: "{type:local-repo,path:/opt/deck/repos/kubernetes}"},
			"spec.source.type":     {Description: "Source type. Currently only `local-repo` is supported.", Example: "local-repo"},
			"spec.source.path":     {Description: "Filesystem path to the pre-prepared local package repository.", Example: "/opt/deck/repos/kubernetes"},
			"spec.restrictToRepos": {Description: "Limit package manager visibility to these repository selectors.", Example: "[offline-kubernetes]"},
			"spec.excludeRepos":    {Description: "Repository selectors to exclude from package resolution.", Example: "[updates]"},
			"spec.timeout":         {Description: "Maximum total duration for package installation.", Example: "20m"},
		},
		Notes: []string{"Use `InstallPackage` during apply to install packages from configured local or mirrored repositories."},
	})
)
