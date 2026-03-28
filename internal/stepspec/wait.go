package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

// Wait bridges convergence gaps between steps.
// @deck.note Keep waits specific so failures identify exactly which dependency did not become ready within the timeout.
// @deck.note Use `initialDelay` when a service emits a transient non-active state immediately after being started.
type Wait struct {
	// Duration between poll attempts.
	// @deck.example 2s
	Interval string `json:"interval"`
	// Deprecated alias for `interval`. Prefer `interval`.
	// @deck.example 2s
	PollInterval string `json:"pollInterval"`
	// Duration to wait before the first poll attempt.
	// @deck.example 1s
	InitialDelay string `json:"initialDelay"`
	// Filesystem path to check.
	// @deck.example /etc/kubernetes/admin.conf
	Path string `json:"path"`
	// List of paths that must all be absent before the step succeeds.
	// @deck.example [/etc/kubernetes/manifests/a.yaml,/etc/kubernetes/manifests/b.yaml]
	Paths []string `json:"paths"`
	// Glob pattern that must resolve to zero matches before the step succeeds.
	// @deck.example /etc/kubernetes/manifests/*.yaml
	Glob string `json:"glob"`
	// Filesystem entry type restriction for path checks.
	// @deck.example file
	Type string `json:"type"`
	// Require the matched file to have non-zero size.
	// @deck.example true
	NonEmpty bool `json:"nonEmpty"`
	// Service name to check.
	// @deck.example containerd
	Name string `json:"name"`
	// Command vector to run on each poll attempt.
	// @deck.example [test,-f,/etc/kubernetes/admin.conf]
	Command []string `json:"command"`
	// Host or IP address for TCP port checks.
	// @deck.example 127.0.0.1
	Address string `json:"address"`
	// TCP port number to check.
	// @deck.example 6443
	Port string `json:"port"`
	// Maximum total duration to wait before the step fails.
	// @deck.example 5m
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[Wait](stepmeta.Definition{
	Kind:        "WaitForService",
	Family:      "wait",
	FamilyTitle: "Wait",
	DocsPage:    "wait",
	DocsOrder:   10,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "wait.service-active.schema.json",
	Summary:     "Wait until a systemd service reports active.",
	WhenToUse:   "Use this after service restarts or runtime configuration changes that take time to settle.",
	Example:     "kind: WaitForService\nspec:\n  name: containerd\n  interval: 2s\n  timeout: 2m",
	Notes: []string{
		"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.",
		"Keep waits specific so failures identify exactly which dependency did not become ready within the timeout.",
		"Use `initialDelay` when a service emits a transient non-active state immediately after being started.",
	},
})

var _ = stepmeta.MustRegister[Wait](stepmeta.Definition{
	Kind:        "WaitForCommand",
	Family:      "wait",
	FamilyTitle: "Wait",
	DocsPage:    "wait",
	DocsOrder:   20,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "wait.command.schema.json",
	Summary:     "Wait until a command exits successfully.",
	WhenToUse:   "Use this when a dependent step should wait for a local command-based condition to succeed.",
	Example:     "kind: WaitForCommand\nspec:\n  command: [test, -f, /etc/kubernetes/admin.conf]\n  interval: 2s\n  timeout: 2m",
	Notes: []string{
		"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.",
		"Keep waits specific so failures identify exactly which dependency did not become ready within the timeout.",
	},
})

var _ = stepmeta.MustRegister[Wait](stepmeta.Definition{
	Kind:        "WaitForFile",
	Family:      "wait",
	FamilyTitle: "Wait",
	DocsPage:    "wait",
	DocsOrder:   30,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "wait.file-exists.schema.json",
	Summary:     "Wait until a file or directory exists.",
	WhenToUse:   "Use this when a prior step produces a file that later steps depend on.",
	Example:     "kind: WaitForFile\nspec:\n  path: /etc/kubernetes/admin.conf\n  type: file\n  nonEmpty: true\n  interval: 2s\n  timeout: 5m",
	Notes: []string{
		"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.",
		"Use `nonEmpty` when waiting on a file that is written progressively.",
	},
})

var _ = stepmeta.MustRegister[Wait](stepmeta.Definition{
	Kind:        "WaitForMissingFile",
	Family:      "wait",
	FamilyTitle: "Wait",
	DocsPage:    "wait",
	DocsOrder:   40,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "wait.file-absent.schema.json",
	Summary:     "Wait until a file or directory is absent.",
	WhenToUse:   "Use this when a cleanup step needs to finish before later steps continue.",
	Example:     "kind: WaitForMissingFile\nspec:\n  path: /var/lib/etcd/member\n  interval: 2s\n  timeout: 2m",
	Notes: []string{
		"Use `paths` or `glob` when multiple files must disappear before the step can succeed.",
	},
})

var _ = stepmeta.MustRegister[Wait](stepmeta.Definition{
	Kind:        "WaitForTCPPort",
	Family:      "wait",
	FamilyTitle: "Wait",
	DocsPage:    "wait",
	DocsOrder:   50,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "wait.tcp-port-open.schema.json",
	Summary:     "Wait until a TCP port opens.",
	WhenToUse:   "Use this when a service must become reachable before later steps continue.",
	Example:     "kind: WaitForTCPPort\nspec:\n  port: \"6443\"\n  interval: 2s\n  timeout: 5m",
})

var _ = stepmeta.MustRegister[Wait](stepmeta.Definition{
	Kind:        "WaitForMissingTCPPort",
	Family:      "wait",
	FamilyTitle: "Wait",
	DocsPage:    "wait",
	DocsOrder:   60,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "wait.tcp-port-closed.schema.json",
	Summary:     "Wait until a TCP port closes.",
	WhenToUse:   "Use this when a process must fully stop before a later step continues.",
	Example:     "kind: WaitForMissingTCPPort\nspec:\n  port: \"10250\"\n  interval: 2s\n  timeout: 2m",
})
