package stepspec

import "github.com/Airgap-Castaways/deck/internal/stepmeta"

// Run an explicit command as an escape hatch.
// @deck.when Use this only when no typed step expresses the change clearly enough.
// @deck.note Prefer a typed step kind over `Command` whenever one is available.
// @deck.note Use `spec.timeout` to bound commands that may hang instead of relying only on the outer step timeout.
// @deck.example
// kind: Command
// spec:
//
//	command: [systemctl, status, containerd]
//	timeout: 30s
type Command struct {
	// Command vector to execute. The first element is the binary and the rest are arguments.
	// @deck.example [systemctl,restart,containerd]
	Command []string `json:"command"`
	// Additional environment variables passed to the command process as key-value pairs.
	// @deck.example {KUBECONFIG:/etc/kubernetes/admin.conf}
	Env map[string]string `json:"env"`
	// Prepend `sudo` before the command vector. Defaults to `false`.
	// @deck.example false
	Sudo bool `json:"sudo"`
	// Maximum duration for the command before it is killed. Overrides the step-level `timeout`.
	// @deck.example 30s
	Timeout string `json:"timeout"`
}

var _ = stepmeta.MustRegister[Command](stepmeta.Definition{
	Kind:        "Command",
	Family:      "command",
	FamilyTitle: "Command",
	DocsPage:    "command",
	DocsOrder:   10,
	Visibility:  "public",
	Roles:       []string{"apply"},
	SchemaFile:  "command.schema.json",
	Ask: stepmeta.AskMetadata{
		MatchSignals: []string{"shell", "command", "script", "escape hatch"},
		KeyFields:    []string{"spec.command", "spec.env", "spec.sudo", "spec.timeout"},
		QualityRules: []stepmeta.QualityRule{{Trigger: "typed-preferred", Message: "Prefer a typed step when one clearly matches the requested host action instead of using Command only.", Level: "advisory"}},
		AntiSignals:  []string{"typed", "typed steps", "where possible"},
	},
})

type CheckHost struct {
	// Named checks to run against the local host.
	// @deck.example [os,arch,swap]
	Checks []string `json:"checks"`
	// Binary names to verify are present in `PATH`.
	// @deck.example [kubeadm,kubelet,kubectl]
	Binaries []string `json:"binaries"`
	// Stop on the first failing check rather than running all checks.
	// @deck.example true
	FailFast *bool `json:"failFast"`
}

var _ = stepmeta.MustRegister[CheckHost](stepmeta.Definition{
	Kind:        "CheckHost",
	Family:      "host-check",
	FamilyTitle: "HostCheck",
	DocsPage:    "host-check",
	DocsOrder:   10,
	Visibility:  "public",
	Roles:       []string{"apply", "prepare"},
	Outputs:     []string{"passed", "failedChecks"},
	SchemaFile:  "host-check.schema.json",
	Summary:     "Validate host suitability checks on the current node.",
	WhenToUse:   "Use this near the start of apply workflows, or optional prepare preflight, to fail early when host prerequisites are not met. Host facts remain available through runtime.host without register.",
	Example:     "kind: CheckHost\nspec:\n  checks: [os, arch, swap]\n  failFast: true",
	Ask: stepmeta.AskMetadata{
		MatchSignals:    []string{"host", "preflight", "rhel", "rocky", "ubuntu", "air-gapped", "single-node"},
		KeyFields:       []string{"spec.checks", "spec.binaries", "spec.failFast"},
		CommonMistakes:  []string{"Use spec.checks as a YAML string array such as [os, arch, swap].", "Do not invent nested objects like spec.os or object items under spec.checks.", "Use CheckHost for suitability validation; use runtime.host.* for detected host branching."},
		RepairHints:     []string{"For CheckHost, use spec.checks as a string array like [os, arch, swap].", "If binary presence matters, keep names under spec.binaries and include binaries in spec.checks.", "Do not add vars like osFamily for local host branching; use runtime.host.os.family instead."},
		ValidationHints: []stepmeta.ValidationHint{{ErrorContains: "checkhost", Fix: "For CheckHost, use spec.checks as a YAML string array like [os, arch, swap]."}, {ErrorContains: "checks is required", Fix: "CheckHost requires spec.checks. Example: spec: {checks: [os, arch, swap]}."}, {ErrorContains: "additional property os is not allowed", Fix: "Do not use spec.os for CheckHost; put named checks under spec.checks instead."}, {ErrorContains: "spec.checks.0: invalid type", Fix: "Each CheckHost spec.checks item must be a plain string such as os or arch, not an object."}},
	},
})
