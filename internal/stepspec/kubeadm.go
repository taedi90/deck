package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

// Run kubeadm init and write the join command to a file.
// @deck.when Use this to bootstrap a control-plane node after host prerequisites are ready.
// @deck.note When `skipIfAdminConfExists` skips the step, deck does not create a new join artifact unless the file already exists.
// @deck.note Load prepared control-plane images with `LoadImage` before bootstrap rather than relying on kubeadm image pulls.
// @deck.example
// kind: InitKubeadm
// spec:
//
//	outputJoinFile: /tmp/deck/join.txt
//	podNetworkCIDR: 10.244.0.0/16
type KubeadmInit struct {
	// Path where the generated join command is written after init.
	// @deck.example /tmp/deck/join.txt
	OutputJoinFile string `json:"outputJoinFile"`
	// Skip init if `/etc/kubernetes/admin.conf` already exists.
	// @deck.example true
	SkipIfAdminConfExists *bool `json:"skipIfAdminConfExists"`
	// CRI socket path passed to kubeadm.
	// @deck.example unix:///run/containerd/containerd.sock
	CriSocket string `json:"criSocket"`
	// Kubernetes version string passed to kubeadm.
	// @deck.example v1.30.1
	KubernetesVersion string `json:"kubernetesVersion"`
	// Path to an explicit kubeadm config file.
	// @deck.example /tmp/deck/kubeadm.conf
	ConfigFile string `json:"configFile"`
	// Inline kubeadm config template or `default`.
	// @deck.example default
	ConfigTemplate string `json:"configTemplate"`
	// CIDR range for the pod network.
	// @deck.example 10.244.0.0/16
	PodNetworkCIDR string `json:"podNetworkCIDR"`
	// API server advertise address or `auto`.
	// @deck.example auto
	AdvertiseAddress string `json:"advertiseAddress"`
	// Preflight checks to suppress.
	// @deck.example [swap]
	IgnorePreflightErrors []string `json:"ignorePreflightErrors"`
	// Additional flags passed directly to kubeadm.
	// @deck.example [--skip-phases=addon/kube-proxy]
	ExtraArgs []string `json:"extraArgs"`
	// Maximum total duration for the init step.
	// @deck.example 15m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[KubeadmInit](stepmeta.Definition{
	Kind:        "InitKubeadm",
	Family:      "kubeadm",
	FamilyTitle: "Kubeadm",
	DocsPage:    "kubeadm",
	DocsOrder:   10,
	Visibility:  "public",
	Roles:       []string{"apply"},
	Outputs:     []string{"joinFile"},
	SchemaFile:  "kubeadm.init.schema.json",
	Ask:         stepmeta.AskMetadata{KeyFields: []string{"spec.outputJoinFile", "spec.configFile", "spec.kubernetesVersion", "spec.advertiseAddress", "spec.podNetworkCIDR"}},
})

// Run kubeadm join for a worker or additional control-plane node.
// @deck.when Use this after a bootstrap node has produced a valid join file or config.
// @deck.note Provide exactly one of `joinFile` or `configFile`.
// @deck.example
// kind: JoinKubeadm
// spec:
//
//	configFile: /tmp/deck/kubeadm-join.yaml
//	extraArgs: [--skip-phases=preflight]
type KubeadmJoin struct {
	// Path to the join command file produced by a prior init run.
	// @deck.example /tmp/deck/join.txt
	JoinFile string `json:"joinFile"`
	// Path to an explicit kubeadm join config file.
	// @deck.example /tmp/deck/kubeadm-join.yaml
	ConfigFile string `json:"configFile"`
	// Join as an additional control-plane member instead of a worker.
	// @deck.example false
	AsControlPlane bool `json:"asControlPlane"`
	// Additional flags passed directly to kubeadm join.
	// @deck.example [--skip-phases=preflight]
	ExtraArgs []string `json:"extraArgs"`
	// Maximum total duration for the join step.
	// @deck.example 15m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[KubeadmJoin](stepmeta.Definition{
	Kind:        "JoinKubeadm",
	Family:      "kubeadm",
	FamilyTitle: "Kubeadm",
	DocsPage:    "kubeadm",
	DocsOrder:   20,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "kubeadm.join.schema.json",
	Ask:         stepmeta.AskMetadata{KeyFields: []string{"spec.joinFile", "spec.configFile", "spec.asControlPlane", "spec.extraArgs"}},
})

// Run kubeadm reset and optional cleanup steps.
// @deck.when Use this to tear down an existing kubeadm-managed node safely.
// @deck.note This step focuses on cleanup and convergence after kubeadm reset.
// @deck.example
// kind: ResetKubeadm
// spec:
//
//	force: true
//	removePaths: [/etc/cni/net.d, /var/lib/etcd]
type KubeadmReset struct {
	// Pass `--force` to kubeadm reset.
	// @deck.example true
	Force bool `json:"force"`
	// Continue with cleanup even if kubeadm reset itself fails.
	// @deck.example true
	IgnoreErrors bool `json:"ignoreErrors"`
	// Stop kubelet before running reset.
	// @deck.example true
	StopKubelet *bool `json:"stopKubelet"`
	// CRI socket path passed to kubeadm.
	// @deck.example unix:///run/containerd/containerd.sock
	CriSocket string `json:"criSocket"`
	// Additional flags passed directly to kubeadm reset.
	// @deck.example [--skip-phases=preflight]
	ExtraArgs []string `json:"extraArgs"`
	// Directories to delete during cleanup.
	// @deck.example [/etc/cni/net.d,/var/lib/etcd]
	RemovePaths []string `json:"removePaths"`
	// Files to delete during cleanup.
	// @deck.example [/etc/kubernetes/admin.conf]
	RemoveFiles []string `json:"removeFiles"`
	// Containers to stop and remove during cleanup.
	// @deck.example [kube-apiserver,etcd]
	CleanupContainers []string `json:"cleanupContainers"`
	// Runtime service to restart after cleanup.
	// @deck.example containerd
	RestartRuntimeManageService string `json:"restartRuntimeService"`
	// Wait until the runtime service reports active.
	// @deck.example true
	WaitForRuntimeService bool `json:"waitForRuntimeService"`
	// Poll runtime readiness after cleanup.
	// @deck.example true
	WaitForRuntimeReady bool `json:"waitForRuntimeReady"`
	// Glob that must resolve to zero matches before the step succeeds.
	// @deck.example /etc/kubernetes/manifests/*.yaml
	WaitForMissingManifestsGlob string `json:"waitForMissingManifestsGlob"`
	// Stop kubelet again after runtime verification completes.
	// @deck.example true
	StopKubeletAfterReset bool `json:"stopKubeletAfterReset"`
	// Containers that must no longer exist after cleanup.
	// @deck.example [kube-apiserver,etcd]
	VerifyContainersAbsent []string `json:"verifyContainersAbsent"`
	// Optional reset report file written after cleanup.
	// @deck.example /tmp/deck/reports/reset-state.txt
	ReportFile string `json:"reportFile"`
	// Value written into the reset report as `resetReason`.
	// @deck.example node-reset-acceptance
	ReportResetReason string `json:"reportResetReason"`
	// Maximum total duration for the reset step.
	// @deck.example 15m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[KubeadmReset](stepmeta.Definition{
	Kind:        "ResetKubeadm",
	Family:      "kubeadm",
	FamilyTitle: "Kubeadm",
	DocsPage:    "kubeadm",
	DocsOrder:   30,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "kubeadm.reset.schema.json",
})

// Run kubeadm upgrade apply and optional kubelet restart.
// @deck.when Use this to upgrade a local kubeadm-managed control-plane node with a typed workflow step.
// @deck.note Restart kubelet after a successful upgrade unless a separate service step owns that lifecycle.
// @deck.example
// kind: UpgradeKubeadm
// spec:
//
//	kubernetesVersion: v1.31.0
//	ignorePreflightErrors: [Swap]
type KubeadmUpgrade struct {
	// Kubernetes version string passed to kubeadm upgrade.
	// @deck.example v1.31.0
	KubernetesVersion string `json:"kubernetesVersion"`
	// Preflight checks to suppress during upgrade.
	// @deck.example [Swap]
	IgnorePreflightErrors []string `json:"ignorePreflightErrors"`
	// Additional flags passed directly to kubeadm upgrade.
	// @deck.example [--allow-experimental-upgrades]
	ExtraArgs []string `json:"extraArgs"`
	// Restart kubelet after a successful upgrade.
	// @deck.example true
	RestartKubelet *bool `json:"restartKubelet"`
	// Service name restarted after a successful upgrade.
	// @deck.example kubelet
	KubeletService string `json:"kubeletService"`
	// Maximum total duration for the upgrade step.
	// @deck.example 30m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[KubeadmUpgrade](stepmeta.Definition{
	Kind:        "UpgradeKubeadm",
	Family:      "kubeadm",
	FamilyTitle: "Kubeadm",
	DocsPage:    "kubeadm",
	DocsOrder:   40,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "kubeadm.upgrade.schema.json",
	Ask:         stepmeta.AskMetadata{KeyFields: []string{"spec.kubernetesVersion", "spec.ignorePreflightErrors", "spec.restartKubelet", "spec.kubeletService"}},
})
