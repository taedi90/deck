package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

// Start, stop, enable, disable, restart, or reload local services.
// @deck.when Use this after config changes that need a service lifecycle action.
// @deck.example
// kind: ManageService
// spec:
//
//	name: containerd
//	enabled: true
//	state: started
type ManageService struct {
	// Single service name to manage. Use `name` or `names`, not both.
	// @deck.example containerd
	Name string `json:"name"`
	// Multiple service names managed in one step. Use `name` or `names`, not both.
	// @deck.example [firewalld,ufw]
	Names []string `json:"names"`
	// Run `systemctl daemon-reload` before applying state changes.
	// @deck.example true
	DaemonReload bool `json:"daemonReload"`
	// Only manage the service if it exists on the host.
	// @deck.example true
	IfExists bool `json:"ifExists"`
	// Suppress errors when the service is not found.
	// @deck.example true
	IgnoreMissing bool `json:"ignoreMissing"`
	// Whether the service should be enabled on boot.
	// @deck.example true
	Enabled *bool `json:"enabled"`
	// Desired service state.
	// @deck.example started
	State string `json:"state"`
	// Maximum total duration for service operations.
	// @deck.example 5m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[ManageService](stepmeta.Definition{Kind: "ManageService", Family: "service", FamilyTitle: "Service", DocsPage: "service", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"name", "names"}, SchemaFile: "service.schema.json", Ask: stepmeta.AskMetadata{MatchSignals: []string{"service", "enable", "restart", "reload", "systemctl"}, KeyFields: []string{"spec.name", "spec.names", "spec.state", "spec.enabled"}}})

// Enable or disable swap and its persistence.
// @deck.when Use this for Kubernetes-oriented host prep where swap policy matters.
// @deck.example
// kind: Swap
// spec:
//
//	disable: true
//	persist: true
type Swap struct {
	// Disable all active swap devices with `swapoff -a`.
	// @deck.example true
	Disable *bool `json:"disable"`
	// Comment out swap entries in fstab so swap stays off after reboot.
	// @deck.example true
	Persist *bool `json:"persist"`
	// Path to the fstab file.
	// @deck.example /etc/fstab
	FstabPath string `json:"fstabPath"`
	// Maximum total duration for swap operations.
	// @deck.example 2m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[Swap](stepmeta.Definition{Kind: "Swap", Family: "swap", FamilyTitle: "Swap", DocsPage: "swap", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, SchemaFile: "swap.schema.json"})

// Load and persist kernel modules.
// @deck.when Use this for modules that must be present before networking or container runtime setup.
// @deck.example
// kind: KernelModule
// spec:
//
//	name: br_netfilter
//	load: true
//	persist: true
//	persistFile: /etc/modules-load.d/k8s.conf
type KernelModule struct {
	// Single module name to manage. Use `name` or `names`, not both.
	// @deck.example br_netfilter
	Name string `json:"name"`
	// Multiple module names managed in one step. Use `name` or `names`, not both.
	// @deck.example [overlay,br_netfilter]
	Names []string `json:"names"`
	// Run `modprobe` to load the module immediately.
	// @deck.example true
	Load *bool `json:"load"`
	// Persist the module under `/etc/modules-load.d/`.
	// @deck.example true
	Persist *bool `json:"persist"`
	// Path to the persistence file written when `persist` is true.
	// @deck.example /etc/modules-load.d/k8s.conf
	PersistFile string `json:"persistFile"`
	// Maximum total duration for module operations.
	// @deck.example 2m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[KernelModule](stepmeta.Definition{Kind: "KernelModule", Family: "kernel-module", FamilyTitle: "KernelModule", DocsPage: "kernel-module", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"name", "names"}, SchemaFile: "kernel-module.schema.json", Ask: stepmeta.AskMetadata{MatchSignals: []string{"kernel", "module", "br_netfilter", "overlay", "kubernetes"}}})

// Write and optionally apply sysctl values.
// @deck.when Use this for kernel tunables that must survive reboot and may need immediate application.
// @deck.example
// kind: Sysctl
// spec:
//
//	writeFile: /etc/sysctl.d/99-kubernetes-cri.conf
//	apply: true
//	values:
//	  net.ipv4.ip_forward: 1
type Sysctl struct {
	// Map of sysctl key-value pairs to write.
	// @deck.example {net.ipv4.ip_forward:1,net.bridge.bridge-nf-call-iptables:1}
	Values map[string]any `json:"values"`
	// Path to the sysctl file written with the given values.
	// @deck.example /etc/sysctl.d/99-k8s.conf
	WriteFile string `json:"writeFile"`
	// Run `sysctl -p <writeFile>` after writing.
	// @deck.example true
	Apply bool `json:"apply"`
	// Maximum total duration for sysctl operations.
	// @deck.example 2m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[Sysctl](stepmeta.Definition{Kind: "Sysctl", Family: "sysctl", FamilyTitle: "Sysctl", DocsPage: "sysctl", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, SchemaFile: "sysctl.schema.json"})

// Write a systemd unit file on the node.
// @deck.when Use this when workflows need to install or override a custom unit definition.
// @deck.note Use `ManageService` separately to enable, start, restart, or reload the unit after it is written.
// @deck.example
// kind: WriteSystemdUnit
// spec:
//
//	path: /etc/systemd/system/kubelet.service
//	template: |
//	  [Unit]
//	  Description=Kubelet
//	daemonReload: true
type WriteSystemdUnit struct {
	// Destination path for the unit file on the node.
	// @deck.example /etc/systemd/system/kubelet.service
	Path string `json:"path"`
	// Inline unit file content written verbatim to `path`.
	// @deck.example
	// [Unit]
	// Description=kubelet
	Content string `json:"content"`
	// Inline multi-line unit content rendered with the current vars before writing.
	// @deck.example
	// [Service]
	// Environment=NODE_IP={{ .vars.nodeIP }}
	Template string `json:"template"`
	// File permissions applied to the unit file in octal notation.
	// @deck.example 0644
	Mode string `json:"mode"`
	// Run `systemctl daemon-reload` after writing the unit file.
	// @deck.example true
	DaemonReload bool `json:"daemonReload"`
	// Maximum total duration for the write and reload operations.
	// @deck.example 2m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[WriteSystemdUnit](stepmeta.Definition{Kind: "WriteSystemdUnit", Family: "systemd-unit", FamilyTitle: "SystemdUnit", DocsPage: "systemd-unit", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "systemd-unit.schema.json", Example: "kind: WriteSystemdUnit\nspec:\n  path: /etc/systemd/system/kubelet.service\n  template: |\n    [Unit]\n    Description=Kubelet\n  daemonReload: true", Notes: []string{"`WriteSystemdUnit` only writes the unit file and optionally performs `daemonReload`.", "Use `ManageService` separately to enable, start, restart, or reload the unit after it is written."}})

// Ensure a directory exists with an optional mode.
// @deck.when Use this before writing files or placing extracted content.
// @deck.example
// kind: EnsureDirectory
// spec:
//
//	path: /home/vagrant/.kube
//	mode: "0755"
type EnsureDirectory struct {
	// Directory path to create if it does not already exist.
	// @deck.example /var/lib/deck
	Path string `json:"path"`
	// Directory permissions in octal notation.
	// @deck.example 0755
	Mode string `json:"mode"`
}

var _ = stepmeta.MustRegister[EnsureDirectory](stepmeta.Definition{Kind: "EnsureDirectory", Family: "directory", FamilyTitle: "Directory", DocsPage: "directory", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "directory.schema.json"})

// Create or replace a symbolic link.
// @deck.when Use this when tools or runtimes expect a stable path alias.
// @deck.example
// kind: CreateSymlink
// spec:
//
//	path: /usr/bin/runc
//	target: /usr/local/sbin/runc
//	force: true
type CreateSymlink struct {
	// Path where the symbolic link will be created.
	// @deck.example /usr/bin/runc
	Path string `json:"path"`
	// Path that the symbolic link points to.
	// @deck.example /usr/local/sbin/runc
	Target string `json:"target"`
	// Remove an existing file or link at `path` before creating the new link.
	// @deck.example true
	Force bool `json:"force"`
	// Create parent directories for `path` if needed.
	// @deck.example true
	CreateParent bool `json:"createParent"`
	// Fail if `target` does not exist when the link is created.
	// @deck.example true
	RequireTarget bool `json:"requireTarget"`
	// Treat a missing target as a no-op instead of an error.
	// @deck.example true
	IgnoreMissingTarget bool `json:"ignoreMissingTarget"`
}

var _ = stepmeta.MustRegister[CreateSymlink](stepmeta.Definition{Kind: "CreateSymlink", Family: "symlink", FamilyTitle: "Symlink", DocsPage: "symlink", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "symlink.schema.json"})

// Write deb or rpm repository definitions.
// @deck.when Use this before refreshing caches or installing packages from a local mirror.
// @deck.note Keep repository definitions mirror-specific rather than mutating the host's default online sources.
// @deck.example
// kind: ConfigureRepository
// spec:
//
//	format: deb
//	path: /etc/apt/sources.list.d/offline.list
//	repositories:
//	  - baseurl: http://repo.local/debian
//	    trusted: true
type ConfigureRepository struct {
	// Repository file format to write.
	// @deck.example deb
	Format string `json:"format"`
	// Explicit output path for the generated repository file.
	// @deck.example /etc/apt/sources.list.d/offline.list
	Path string `json:"path"`
	// File permissions applied to the generated repository file.
	// @deck.example 0644
	Mode string `json:"mode"`
	// Replace an existing repository file at the target path.
	// @deck.example true
	ReplaceExisting bool `json:"replaceExisting"`
	// Disable all existing repository definitions before writing the new one.
	// @deck.example true
	DisableExisting bool `json:"disableExisting"`
	// Paths to back up before modifying.
	// @deck.example [/etc/apt/sources.list]
	BackupPaths []string `json:"backupPaths"`
	// Paths to remove before writing the new definition.
	// @deck.example [/etc/apt/sources.list.d/ubuntu.list]
	CleanupPaths []string `json:"cleanupPaths"`
	// Repository entries to write.
	// @deck.example [{baseurl:http://repo.local/debian,trusted:true}]
	Repositories []RepositoryEntry `json:"repositories"`
}

var _ = stepmeta.MustRegister[ConfigureRepository](stepmeta.Definition{Kind: "ConfigureRepository", Family: "repository", FamilyTitle: "Repository", DocsPage: "repository", DocsOrder: 10, Visibility: "public", Roles: []string{"apply"}, Outputs: []string{"path"}, SchemaFile: "repository.configure.schema.json", Notes: []string{"`ConfigureRepository` only writes repository definition files. Use `RefreshRepository` when the package manager needs an explicit metadata refresh.", "Keep repository definitions mirror-specific rather than mutating the host's default online sources."}, Ask: stepmeta.AskMetadata{MatchSignals: []string{"repo", "repository", "mirror", "yum", "dnf", "apt"}, KeyFields: []string{"spec.format", "spec.path", "spec.repositories", "spec.replaceExisting", "spec.cleanupPaths"}, CommonMistakes: []string{"spec.repositories must stay a real YAML array of repository objects, not a scalar shortcut.", "Do not set spec.repositories to `{{ .vars.* }}` or any other whole-value template expression; inline repository objects instead."}, RepairHints: []string{"Inline repository objects under spec.repositories rather than using a scalar or whole-value template."}, ValidationHints: []stepmeta.ValidationHint{{ErrorContains: "invalid map key", Fix: "Do not use whole-value template expressions for spec.repositories; inline YAML repository objects instead."}}, ConstrainedLiteralFields: []stepmeta.ConstrainedLiteralField{{Path: "spec.format", AllowedValues: []string{"auto", "deb", "rpm"}, Guidance: "Keep spec.format as a literal enum, not a vars template."}}}})

type RepositoryEntry struct {
	// RPM repository ID.
	// @deck.example offline-kubernetes
	ID string `json:"id"`
	// Human-readable repository name.
	// @deck.example Offline Kubernetes
	Name string `json:"name"`
	// Base URL for the repository.
	// @deck.example http://repo.local/debian
	BaseURL string `json:"baseurl"`
	// Explicit enabled state for the repository entry.
	// @deck.example true
	Enabled *bool `json:"enabled"`
	// Explicit gpgcheck state for RPM repositories.
	// @deck.example true
	GPGCheck *bool `json:"gpgcheck"`
	// URL or path to the repository GPG key.
	// @deck.example file:///etc/pki/rpm-gpg/RPM-GPG-KEY-offline
	GPGKey string `json:"gpgkey"`
	// Mark a deb repository as trusted.
	// @deck.example true
	Trusted *bool `json:"trusted"`
	// Deb repository suite.
	// @deck.example stable
	Suite string `json:"suite"`
	// Deb repository component.
	// @deck.example main
	Component string `json:"component"`
	// Deb repository type.
	// @deck.example deb
	Type string `json:"type"`
	// Additional rpm-style repository keys.
	// @deck.example {priority:10,module_hotfixes:true}
	Extra map[string]any `json:"extra"`
}

// Refresh package metadata with repo filtering.
// @deck.when Use this after writing repo definitions and before package install steps that depend on fresh metadata.
// @deck.example
// kind: RefreshRepository
// spec:
//
//	manager: apt
//	clean: true
//	update: true
//	restrictToRepos:
//	  - /etc/apt/sources.list.d/offline.list
type RefreshRepository struct {
	// Package manager to use.
	// @deck.example apt
	Manager string `json:"manager"`
	// Run a cache clean before updating metadata.
	// @deck.example true
	Clean bool `json:"clean"`
	// Fetch fresh package metadata from the configured repositories.
	// @deck.example true
	Update bool `json:"update"`
	// Limit the metadata update to these repository selectors.
	// @deck.example [/etc/apt/sources.list.d/offline.list]
	RestrictToRepos []string `json:"restrictToRepos"`
	// Repository selectors to skip during metadata update.
	// @deck.example [updates]
	ExcludeRepos []string `json:"excludeRepos"`
	// Maximum total duration for refresh operations.
	// @deck.example 5m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[RefreshRepository](stepmeta.Definition{Kind: "RefreshRepository", Family: "repository", FamilyTitle: "Repository", DocsPage: "repository", DocsOrder: 20, Visibility: "public", Roles: []string{"apply"}, SchemaFile: "repository.refresh.schema.json", Ask: stepmeta.AskMetadata{MatchSignals: []string{"repo", "repository", "metadata", "refresh", "cache", "dnf", "apt"}, KeyFields: []string{"spec.manager", "spec.clean", "spec.update", "spec.restrictToRepos", "spec.excludeRepos"}, ConstrainedLiteralFields: []stepmeta.ConstrainedLiteralField{{Path: "spec.manager", AllowedValues: []string{"auto", "apt", "dnf"}, Guidance: "Keep spec.manager as a literal enum, not a vars template."}}}})

type InstallPackageSource struct {
	// Source type. Currently only `local-repo` is supported.
	// @deck.example local-repo
	Type string `json:"type"`
	// Filesystem path to the pre-prepared local package repository.
	// @deck.example /opt/deck/repos/kubernetes
	Path string `json:"path"`
}

// Install packages on the local node.
// @deck.when Use this during apply to install packages from configured local or mirrored repositories.
// @deck.example
// kind: InstallPackage
// spec:
//
//	packages: [kubelet, kubeadm, kubectl]
//	source:
//	  type: local-repo
//	  path: /opt/deck/repos/kubernetes
type InstallPackage struct {
	// Local repository source used for the installation.
	// @deck.example {type:local-repo,path:/opt/deck/repos/kubernetes}
	Source *InstallPackageSource `json:"source"`
	// Package names to install.
	// @deck.example [kubelet,kubeadm,kubectl]
	Packages []string `json:"packages"`
	// Limit package manager visibility to these repository selectors.
	// @deck.example [offline-kubernetes]
	RestrictToRepos []string `json:"restrictToRepos"`
	// Repository selectors to exclude from package resolution.
	// @deck.example [updates]
	ExcludeRepos []string `json:"excludeRepos"`
	// Maximum total duration for package installation.
	// @deck.example 20m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[InstallPackage](stepmeta.Definition{Kind: "InstallPackage", Family: "package", FamilyTitle: "Package", DocsPage: "package", DocsOrder: 20, Visibility: "public", Roles: []string{"apply"}, SchemaFile: "package.install.schema.json", Ask: stepmeta.AskMetadata{MatchSignals: []string{"install", "package", "packages", "rpm", "dnf", "apt"}, KeyFields: []string{"spec.packages", "spec.source", "spec.source.type", "spec.source.path", "spec.restrictToRepos", "spec.excludeRepos"}, CommonMistakes: []string{"spec.packages must stay a real YAML array, not a quoted template string.", "Do not set spec.packages to `{{ .vars.* }}` or any other whole-value template expression; inline package items instead.", "Keep constrained enum fields such as spec.backend.mode, spec.backend.runtime, and spec.repo.type as literal allowed values instead of vars templates."}, RepairHints: []string{"Inline concrete YAML arrays for spec.packages rather than using a whole-value template expression.", "Keep enum-like fields such as spec.backend.mode, spec.backend.runtime, and spec.repo.type as literal schema-supported values."}, ValidationHints: []stepmeta.ValidationHint{{ErrorContains: "invalid map key", Fix: "Do not use whole-value template expressions for package arrays; inline YAML list items under spec.packages."}, {ErrorContains: "spec.backend.mode must be one of", Fix: "Keep spec.backend.mode as the literal value `container`; do not replace enum fields with vars templates."}, {ErrorContains: "spec.backend.runtime must be one of", Fix: "Keep spec.backend.runtime as a literal enum such as `docker`, `podman`, or `auto`; do not replace it with a vars template."}, {ErrorContains: "spec.repo.type must be one of", Fix: "Keep spec.repo.type as a literal allowed value such as `rpm` or `deb-flat`; do not replace it with a vars template."}}}})

type DownloadPackageDistro struct {
	// Distribution family used to resolve package tooling.
	// @deck.example rhel
	Family string `json:"family"`
	// Distribution release used for resolver and repo layout selection.
	// @deck.example rocky9
	Release string `json:"release"`
}

type DownloadPackageRepoModule struct {
	// RPM module name to enable.
	// @deck.example container-tools
	Name string `json:"name"`
	// Module stream version paired with the module name.
	// @deck.example 4.0
	Stream string `json:"stream"`
}

type DownloadPackageRepo struct {
	// Repository output type for download repo mode.
	// @deck.example rpm
	Type string `json:"type"`
	// Generate repository metadata after collecting packages.
	// @deck.example true
	Generate bool `json:"generate"`
	// Subdirectory under the generated repo root where packages are written.
	// @deck.example pkgs
	PkgsDir string `json:"pkgsDir"`
	// RPM module streams to enable before resolving downloads.
	// @deck.example [{name:container-tools,stream:4.0}]
	Modules []DownloadPackageRepoModule `json:"modules"`
}

type DownloadPackageBackend struct {
	// Download backend mode.
	// @deck.example container
	Mode string `json:"mode"`
	// Preferred container runtime for the download helper container.
	// @deck.example docker
	Runtime string `json:"runtime"`
	// Container image used for package resolution in download mode.
	// @deck.example rockylinux:9
	Image string `json:"image"`
}

// Download packages into prepared bundle storage.
// @deck.when Use this during prepare to collect package-manager content for offline installation.
// @deck.note Omit `outputDir` unless you need a custom package location.
// @deck.note Use the same package list across `download` and `install` to keep offline parity.
// @deck.example
// kind: DownloadPackage
// spec:
//
//	packages: [podman]
//	distro:
//	  family: rhel
//	  release: rocky9
//	repo:
//	  type: rpm
//	  modules:
//	    - name: container-tools
//	      stream: "4.0"
//	backend:
//	  mode: container
//	  runtime: docker
//	  image: rockylinux:9
type DownloadPackage struct {
	// Package names to download.
	// @deck.example [kubelet,kubeadm,kubectl]
	Packages []string `json:"packages"`
	// Target distribution hint used to select resolver behavior.
	// @deck.example {family:rhel,release:rocky9}
	Distro DownloadPackageDistro `json:"distro"`
	// Repository settings applied before download.
	// @deck.example {type:rpm,modules:[{name:container-tools,stream:4.0}]}
	Repo DownloadPackageRepo `json:"repo"`
	// Container-based download backend configuration.
	// @deck.example {mode:container,runtime:docker,image:rockylinux:9}
	Backend DownloadPackageBackend `json:"backend"`
	// Optional bundle-relative output directory for downloaded package artifacts.
	// @deck.example packages/kubernetes
	OutputDir string `json:"outputDir"`
	// Maximum total duration for package download operations.
	// @deck.example 30m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[DownloadPackage](stepmeta.Definition{Kind: "DownloadPackage", Family: "package", FamilyTitle: "Package", DocsPage: "package", DocsOrder: 10, Visibility: "public", Roles: []string{"prepare"}, Outputs: []string{"artifacts"}, SchemaFile: "package.download.schema.json", Notes: []string{"Use `DownloadPackage` and `InstallPackage` with `ConfigureRepository` and `RefreshRepository` for a complete typed package-management flow.", "Omit `outputDir` unless you need a custom package location; deck uses `packages/` by default, or `packages/deb/<release>` and `packages/rpm/<release>` when `repo.type` is set.", "Keeping the same package list across `download` and `install` helps maintain offline parity.", "Use `restrictToRepos` on the `InstallPackage` step to prevent the node's default online repos from being consulted during an offline apply.", "When `repo` is set for `DownloadPackage`, deck expects `repo.type` and `distro.release` so it can build a `deb-flat` or `rpm` repository layout.", "Container-backed `DownloadPackage` exports completed artifacts into a host-owned cache and does not bind-mount deb/rpm package-manager cache directories."}, Ask: stepmeta.AskMetadata{MatchSignals: []string{"download", "package", "packages", "rpm", "dnf", "air-gapped", "offline"}, KeyFields: []string{"spec.packages", "spec.distro", "spec.repo", "spec.backend"}, CommonMistakes: []string{"spec.packages must stay a real YAML array, not a quoted template string.", "Do not set spec.packages to `{{ .vars.* }}` or any other whole-value template expression; inline package items instead.", "Keep constrained enum fields such as spec.backend.mode, spec.backend.runtime, and spec.repo.type as literal allowed values instead of vars templates."}, RepairHints: []string{"Inline concrete YAML arrays for spec.packages rather than using a whole-value template expression.", "Keep enum-like fields such as spec.backend.mode, spec.backend.runtime, and spec.repo.type as literal schema-supported values."}, ValidationHints: []stepmeta.ValidationHint{{ErrorContains: "invalid map key", Fix: "Do not use whole-value template expressions for package arrays; inline YAML list items under spec.packages."}, {ErrorContains: "spec.backend.mode must be one of", Fix: "Keep spec.backend.mode as the literal value `container`; do not replace enum fields with vars templates."}, {ErrorContains: "spec.backend.runtime must be one of", Fix: "Keep spec.backend.runtime as a literal enum such as `docker`, `podman`, or `auto`; do not replace it with a vars template."}, {ErrorContains: "spec.repo.type must be one of", Fix: "Keep spec.repo.type as a literal allowed value such as `rpm` or `deb-flat`; do not replace it with a vars template."}}, ConstrainedLiteralFields: []stepmeta.ConstrainedLiteralField{{Path: "spec.backend.mode", AllowedValues: []string{"container"}, Guidance: "Keep spec.backend.mode as a literal enum, not a vars template."}, {Path: "spec.backend.runtime", AllowedValues: []string{"auto", "docker", "podman"}, Guidance: "Keep spec.backend.runtime as a literal enum, not a vars template."}, {Path: "spec.repo.type", AllowedValues: []string{"deb-flat", "rpm"}, Guidance: "Keep spec.repo.type as a literal enum, not a vars template."}}}})
