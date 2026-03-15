package workflowexec

import (
	"sort"
	"strings"
)

type StepContract struct {
	Kind       string
	SchemaFile string
	Roles      map[string]bool
	Outputs    map[string]bool
	Actions    map[string]ActionContract
}

type ActionContract struct {
	Outputs map[string]bool
	Roles   map[string]bool
}

var stepContracts = map[string]StepContract{
	"Inspection": simpleStep("inspection.schema.json", setOf("prepare"), setOf("passed", "failedChecks")),
	"Artifacts":  simpleStep("artifacts.schema.json", setOf("apply"), nil),
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
		"install":  {Outputs: setOf("path"), Roles: setOf("apply")},
		"copy":     {Outputs: setOf("dest"), Roles: setOf("apply")},
		"edit":     {Outputs: setOf("path"), Roles: setOf("apply")},
	}),
	"Repository": familyStep("repository.schema.json", setOf("apply"), map[string]ActionContract{
		"configure": {Outputs: setOf("path"), Roles: setOf("apply")},
	}),
	"Image": familyStep("image.schema.json", setOf("prepare", "apply"), map[string]ActionContract{
		"download": {Outputs: setOf("artifacts"), Roles: setOf("prepare")},
		"present":  {Outputs: nil, Roles: setOf("apply")},
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

var legacyStepKindAliases = map[string]string{
	"CheckHost":           "Inspection",
	"DownloadFile":        "File",
	"DownloadImages":      "Image",
	"DownloadPackages":    "Packages",
	"DownloadK8sPackages": "Packages",
	"InstallArtifacts":    "Artifacts",
	"InstallPackages":     "Packages",
	"WriteFile":           "File",
	"EditFile":            "File",
	"CopyFile":            "File",
	"EnsureDir":           "Directory",
	"InstallFile":         "File",
	"TemplateFile":        "File",
	"RepoConfig":          "Repository",
	"ContainerdConfig":    "Containerd",
	"Modprobe":            "KernelModule",
	"RunCommand":          "Command",
	"VerifyImages":        "Image",
	"KubeadmInit":         "Kubeadm",
	"KubeadmJoin":         "Kubeadm",
	"KubeadmReset":        "Kubeadm",
	"WaitPath":            "Wait",
}

func simpleStep(schema string, roles map[string]bool, outputs map[string]bool) StepContract {
	return StepContract{SchemaFile: schema, Roles: roles, Outputs: outputs}
}

func familyStep(schema string, roles map[string]bool, actions map[string]ActionContract) StepContract {
	return StepContract{SchemaFile: schema, Roles: roles, Actions: actions}
}

func StepSchemaFile(kind string) (string, bool) {
	if canonical, ok := legacyStepKindAliases[kind]; ok {
		kind = canonical
	}
	contract, ok := stepContracts[kind]
	if !ok || contract.SchemaFile == "" {
		return "", false
	}
	return contract.SchemaFile, true
}

func StepKinds() []string {
	kinds := make([]string, 0, len(stepContracts))
	for kind := range stepContracts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func StepAllowedForRole(role, kind string, spec map[string]any) bool {
	kind, spec = normalizeLegacyKindSpec(kind, spec)
	contract, ok := stepContracts[kind]
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
	kind, spec = normalizeLegacyKindSpec(kind, spec)
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
		return "install"
	case "Repository":
		return "configure"
	case "Image":
		if spec != nil {
			if spec["backend"] != nil || spec["output"] != nil {
				return "download"
			}
		}
		return "present"
	case "Packages":
		if spec != nil {
			if spec["backend"] != nil || spec["distro"] != nil || spec["repo"] != nil {
				return "download"
			}
		}
		return "install"
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

func normalizeLegacyKindSpec(kind string, spec map[string]any) (string, map[string]any) {
	canonical, ok := legacyStepKindAliases[kind]
	if !ok {
		return kind, spec
	}
	if spec == nil {
		spec = map[string]any{}
	} else {
		cloned := make(map[string]any, len(spec)+1)
		for k, v := range spec {
			cloned[k] = v
		}
		spec = cloned
	}
	switch kind {
	case "DownloadFile":
		spec["action"] = "download"
	case "DownloadImages":
		spec["action"] = "download"
	case "DownloadPackages":
		spec["action"] = "download"
	case "DownloadK8sPackages":
		spec["action"] = "download"
	case "InstallPackages":
		spec["action"] = "install"
	case "WriteFile", "InstallFile", "TemplateFile":
		spec["action"] = "install"
	case "EditFile":
		spec["action"] = "edit"
	case "CopyFile":
		spec["action"] = "copy"
	case "RepoConfig":
		spec["action"] = "configure"
	case "VerifyImages":
		spec["action"] = "present"
	case "KubeadmInit":
		spec["action"] = "init"
	case "KubeadmJoin":
		spec["action"] = "join"
	case "KubeadmReset":
		spec["action"] = "reset"
	case "WaitPath":
		if action, _ := spec["action"].(string); strings.TrimSpace(action) == "" {
			if state, _ := spec["state"].(string); state == "absent" {
				spec["action"] = "fileAbsent"
			} else {
				spec["action"] = "fileExists"
			}
		}
	}
	return canonical, spec
}

func NormalizeLegacyKindSpec(kind string, spec map[string]any) (string, map[string]any) {
	return normalizeLegacyKindSpec(kind, spec)
}

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
