package workflowexec

type StepContract struct {
	Kind       string
	SchemaFile string
	Roles      map[string]bool
	Outputs    map[string]bool
	Actions    map[string]ActionContract
}

type ActionContract struct {
	Outputs map[string]bool
}

var stepContracts = map[string]StepContract{
	"Inspection":   simpleStep("inspection.schema.json", setOf("prepare"), setOf("passed", "failedChecks")),
	"PackageFetch": simpleStep("package-fetch.schema.json", setOf("prepare"), setOf("artifacts")),
	"ImageFetch":   simpleStep("image-fetch.schema.json", setOf("prepare"), setOf("artifacts")),
	"FileFetch":    simpleStep("file-fetch.schema.json", setOf("prepare", "apply"), setOf("path", "artifacts")),
	"Artifacts":    simpleStep("artifacts.schema.json", setOf("apply"), nil),
	"Packages":     simpleStep("packages.schema.json", setOf("apply"), nil),
	"Directory":    simpleStep("directory.schema.json", setOf("apply"), setOf("path")),
	"Symlink":      simpleStep("symlink.schema.json", setOf("apply"), setOf("path")),
	"SystemdUnit":  simpleStep("systemd-unit.schema.json", setOf("apply"), setOf("path")),
	"Containerd":   simpleStep("containerd.schema.json", setOf("apply"), setOf("path")),
	"PackageCache": simpleStep("package-cache.schema.json", setOf("apply"), nil),
	"Swap":         simpleStep("swap.schema.json", setOf("apply"), nil),
	"KernelModule": simpleStep("kernel-module.schema.json", setOf("apply"), setOf("name", "names")),
	"Command":      simpleStep("command.schema.json", setOf("apply"), nil),
	"Service":      simpleStep("service.schema.json", setOf("apply"), setOf("name")),
	"Sysctl":       simpleStep("sysctl.schema.json", setOf("apply"), nil),
	"File": familyStep("file.schema.json", setOf("apply"), map[string]ActionContract{
		"install": {Outputs: setOf("path")},
		"copy":    {Outputs: setOf("dest")},
		"edit":    {Outputs: setOf("path")},
	}),
	"Repository": familyStep("repository.schema.json", setOf("apply"), map[string]ActionContract{
		"configure": {Outputs: setOf("path")},
	}),
	"Image": familyStep("image.schema.json", setOf("apply"), map[string]ActionContract{
		"present": {Outputs: nil},
	}),
	"Wait": familyStep("wait.schema.json", setOf("apply"), map[string]ActionContract{
		"serviceActive":  {Outputs: nil},
		"commandSuccess": {Outputs: nil},
		"fileExists":     {Outputs: nil},
		"fileAbsent":     {Outputs: nil},
		"tcpPortClosed":  {Outputs: nil},
		"tcpPortOpen":    {Outputs: nil},
	}),
	"Kubeadm": familyStep("kubeadm.schema.json", setOf("apply"), map[string]ActionContract{
		"init":  {Outputs: setOf("joinFile")},
		"join":  {Outputs: nil},
		"reset": {Outputs: nil},
	}),
}

func simpleStep(schema string, roles map[string]bool, outputs map[string]bool) StepContract {
	return StepContract{SchemaFile: schema, Roles: roles, Outputs: outputs}
}

func familyStep(schema string, roles map[string]bool, actions map[string]ActionContract) StepContract {
	return StepContract{SchemaFile: schema, Roles: roles, Actions: actions}
}

func StepSchemaFile(kind string) (string, bool) {
	contract, ok := stepContracts[kind]
	if !ok || contract.SchemaFile == "" {
		return "", false
	}
	return contract.SchemaFile, true
}

func StepAllowedForRole(role, kind string) bool {
	contract, ok := stepContracts[kind]
	if !ok {
		return false
	}
	return contract.Roles[role]
}

func StepHasOutput(kind string, spec map[string]any, output string) bool {
	contract, ok := stepContracts[kind]
	if !ok {
		return false
	}
	if contract.Outputs[output] {
		return true
	}
	if len(contract.Actions) == 0 {
		return false
	}
	action := inferContractAction(kind, spec)
	return contract.Actions[action].Outputs[output]
}

func inferContractAction(kind string, spec map[string]any) string {
	if spec != nil {
		if raw, ok := spec["action"].(string); ok && raw != "" {
			return raw
		}
	}
	switch kind {
	case "File":
		if _, ok := spec["edits"]; ok {
			return "edit"
		}
		if spec != nil {
			if spec["src"] != nil || spec["dest"] != nil {
				return "copy"
			}
		}
		return "install"
	case "Repository":
		return "configure"
	case "Image":
		return "present"
	case "Kubeadm":
		if spec != nil {
			if spec["joinFile"] != nil {
				return "join"
			}
			if spec["outputJoinFile"] != nil {
				return "init"
			}
		}
		return "reset"
	case "Wait":
		if spec != nil {
			if state, _ := spec["state"].(string); state == "absent" {
				return "fileAbsent"
			}
		}
		return "fileExists"
	default:
		return ""
	}
}

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
