package stepspec

var waitFieldDocs = map[string]FieldDoc{
	"spec.name":         {Description: "Service name to check.", Example: "containerd"},
	"spec.command":      {Description: "Command vector to run on each poll attempt. The step succeeds when the command exits 0.", Example: "[test,-f,/etc/kubernetes/admin.conf]"},
	"spec.path":         {Description: "Filesystem path to check.", Example: "/etc/kubernetes/admin.conf"},
	"spec.paths":        {Description: "List of paths that must all be absent before the step succeeds.", Example: "[/etc/kubernetes/manifests/a.yaml,/etc/kubernetes/manifests/b.yaml]"},
	"spec.glob":         {Description: "Glob pattern that must resolve to zero matches before the step succeeds.", Example: "/etc/kubernetes/manifests/*.yaml"},
	"spec.type":         {Description: "Filesystem entry type restriction for path checks.", Example: "file"},
	"spec.nonEmpty":     {Description: "Require the matched file to have non-zero size.", Example: "true"},
	"spec.port":         {Description: "TCP port number to check.", Example: "6443"},
	"spec.address":      {Description: "Host or IP address for TCP port checks.", Example: "127.0.0.1"},
	"spec.interval":     {Description: "Duration between poll attempts.", Example: "2s"},
	"spec.initialDelay": {Description: "Duration to wait before the first poll attempt.", Example: "1s"},
	"spec.timeout":      {Description: "Maximum total duration to wait before failing the step.", Example: "5m"},
	"spec.pollInterval": {Description: "Deprecated alias for `interval`. Use `interval` instead.", Example: "2s"},
}

var (
	_ = registerToolDoc("WaitForService", ToolDocMetadata{Example: "kind: WaitForService\nspec:\n  name: containerd\n  interval: 2s\n  timeout: 2m\n", FieldDocs: waitFieldDocs, Notes: []string{"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.", "Keep waits specific so failures identify exactly which dependency did not become ready within the timeout.", "Use `initialDelay` when a service emits a transient non-active state immediately after being started."}})
	_ = registerToolDoc("WaitForCommand", ToolDocMetadata{Example: "kind: WaitForCommand\nspec:\n  command: [test, -f, /etc/kubernetes/admin.conf]\n  interval: 2s\n  timeout: 2m\n", FieldDocs: waitFieldDocs, Notes: []string{"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.", "Keep waits specific so failures identify exactly which dependency did not become ready within the timeout.", "Use `initialDelay` when a service emits a transient non-active state immediately after being started."}})
	_ = registerToolDoc("WaitForFile", ToolDocMetadata{Example: "kind: WaitForFile\nspec:\n  path: /etc/kubernetes/admin.conf\n  type: file\n  nonEmpty: true\n  interval: 2s\n  timeout: 5m\n", FieldDocs: waitFieldDocs, Notes: []string{"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.", "Keep waits specific so failures identify exactly which dependency did not become ready within the timeout.", "Use `nonEmpty` when waiting on a file that is written progressively."}})
	_ = registerToolDoc("WaitForMissingFile", ToolDocMetadata{Example: "kind: WaitForMissingFile\nspec:\n  path: /var/lib/etcd/member\n  interval: 2s\n  timeout: 2m\n", FieldDocs: waitFieldDocs, Notes: []string{"Use `paths` or `glob` when multiple files must disappear before the step can succeed."}})
	_ = registerToolDoc("WaitForTCPPort", ToolDocMetadata{Example: "kind: WaitForTCPPort\nspec:\n  port: \"6443\"\n  interval: 2s\n  timeout: 5m\n", FieldDocs: waitFieldDocs, Notes: []string{"`Wait` bridges convergence gaps between steps. It should not replace the configuration action itself.", "Keep waits specific so failures identify exactly which dependency did not become ready within the timeout."}})
	_ = registerToolDoc("WaitForMissingTCPPort", ToolDocMetadata{Example: "kind: WaitForMissingTCPPort\nspec:\n  port: \"10250\"\n  interval: 2s\n  timeout: 2m\n", FieldDocs: waitFieldDocs, Notes: []string{"Use this when a process must fully stop before a later step continues."}})
)
