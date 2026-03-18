package workflowexec

import "sort"

type StepContract struct {
	SchemaFile string
	Roles      map[string]bool
	Outputs    map[string]bool
	Actions    map[string]ActionContract
}

type ActionContract struct {
	Outputs map[string]bool
	Roles   map[string]bool
}

func stepContracts() map[string]StepContract {
	return map[string]StepContract{
		"Checks":    simpleStep("checks.schema.json", setOf("prepare"), setOf("passed", "failedChecks")),
		"Artifacts": simpleStep("artifacts.schema.json", setOf("apply"), nil),
		"Packages": familyStep("packages.schema.json", setOf("prepare", "apply"), map[string]ActionContract{
			"download": {Outputs: setOf("artifacts"), Roles: setOf("prepare")},
			"install":  {Outputs: nil, Roles: setOf("apply")},
		}),
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
		"File": familyStep("file.schema.json", setOf("prepare", "apply"), map[string]ActionContract{
			"download": {Outputs: setOf("path", "artifacts"), Roles: setOf("prepare", "apply")},
			"write":    {Outputs: setOf("path"), Roles: setOf("apply")},
			"copy":     {Outputs: setOf("dest"), Roles: setOf("apply")},
			"edit":     {Outputs: setOf("path"), Roles: setOf("apply")},
		}),
		"Repository": familyStep("repository.schema.json", setOf("apply"), map[string]ActionContract{
			"configure": {Outputs: setOf("path"), Roles: setOf("apply")},
		}),
		"Image": familyStep("image.schema.json", setOf("prepare", "apply"), map[string]ActionContract{
			"download": {Outputs: setOf("artifacts"), Roles: setOf("prepare")},
			"verify":   {Outputs: nil, Roles: setOf("apply")},
		}),
		"Wait": familyStep("wait.schema.json", setOf("apply"), map[string]ActionContract{
			"serviceActive":  {Outputs: nil, Roles: setOf("apply")},
			"commandSuccess": {Outputs: nil, Roles: setOf("apply")},
			"fileExists":     {Outputs: nil, Roles: setOf("apply")},
			"fileAbsent":     {Outputs: nil, Roles: setOf("apply")},
			"tcpPortClosed":  {Outputs: nil, Roles: setOf("apply")},
			"tcpPortOpen":    {Outputs: nil, Roles: setOf("apply")},
		}),
		"Kubeadm": familyStep("kubeadm.schema.json", setOf("apply"), map[string]ActionContract{
			"init":  {Outputs: setOf("joinFile"), Roles: setOf("apply")},
			"join":  {Outputs: nil, Roles: setOf("apply")},
			"reset": {Outputs: nil, Roles: setOf("apply")},
		}),
	}
}

func simpleStep(schema string, roles map[string]bool, outputs map[string]bool) StepContract {
	return StepContract{SchemaFile: schema, Roles: roles, Outputs: outputs}
}

func familyStep(schema string, roles map[string]bool, actions map[string]ActionContract) StepContract {
	return StepContract{SchemaFile: schema, Roles: roles, Actions: actions}
}

func StepSchemaFile(kind string) (string, bool) {
	contract, ok := stepContracts()[kind]
	if !ok || contract.SchemaFile == "" {
		return "", false
	}
	return contract.SchemaFile, true
}

func StepContractForKind(kind string) (StepContract, bool) {
	contract, ok := stepContracts()[kind]
	return contract, ok
}

func StepKinds() []string {
	contracts := stepContracts()
	kinds := make([]string, 0, len(contracts))
	for kind := range contracts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func StepAllowedForRole(role, kind string, spec map[string]any) bool {
	contract, ok := stepContracts()[kind]
	if !ok {
		return false
	}
	if spec != nil {
		if _, ok := spec["action"].(string); !ok || spec["action"] == "" {
			switch kind {
			case "File":
				if role == "prepare" {
					return spec["source"] != nil || spec["output"] != nil
				}
				return contract.Roles[role]
			case "Image":
				if role == "prepare" {
					return spec["backend"] != nil || spec["output"] != nil || spec["images"] != nil
				}
				return contract.Roles[role]
			case "Packages":
				return contract.Roles[role]
			}
		}
	}
	action := inferContractAction(kind, spec)
	if action != "" && len(contract.Actions) > 0 {
		if actionContract, ok := contract.Actions[action]; ok && len(actionContract.Roles) > 0 {
			return actionContract.Roles[role]
		}
	}
	return contract.Roles[role]
}

func StepHasOutput(kind string, spec map[string]any, output string) bool {
	contract, ok := stepContracts()[kind]
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

func InferStepAction(kind string, spec map[string]any) string {
	return inferContractAction(kind, spec)
}

func inferContractAction(kind string, spec map[string]any) string {
	if spec != nil {
		if raw, ok := spec["action"].(string); ok && raw != "" {
			return raw
		}
	}
	switch kind {
	case "File":
		if spec != nil {
			if spec["source"] != nil || spec["output"] != nil {
				return "download"
			}
		}
		if _, ok := spec["edits"]; ok {
			return "edit"
		}
		if spec != nil {
			if spec["src"] != nil || spec["dest"] != nil {
				return "copy"
			}
		}
		return "write"
	case "Repository":
		return "configure"
	case "Image":
		if spec != nil {
			if spec["backend"] != nil || spec["output"] != nil {
				return "download"
			}
		}
		return "verify"
	case "Packages":
		if spec != nil {
			if spec["backend"] != nil || spec["distro"] != nil || spec["repo"] != nil {
				return "download"
			}
		}
		return "install"
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
