package workflowexec

type StepContract struct {
	Kind       string
	SchemaFile string
	Roles      map[string]bool
	Outputs    map[string]bool
}

var stepContracts = map[string]StepContract{
	"CheckHost": {
		Kind:       "CheckHost",
		SchemaFile: "check-host.schema.json",
		Roles:      setOf("prepare"),
		Outputs:    setOf("passed", "failedChecks"),
	},
	"DownloadPackages": {
		Kind:       "DownloadPackages",
		SchemaFile: "download-packages.schema.json",
		Roles:      setOf("prepare"),
		Outputs:    setOf("artifacts"),
	},
	"DownloadImages": {
		Kind:       "DownloadImages",
		SchemaFile: "download-images.schema.json",
		Roles:      setOf("prepare"),
		Outputs:    setOf("artifacts"),
	},
	"DownloadFile": {
		Kind:       "DownloadFile",
		SchemaFile: "download-file.schema.json",
		Roles:      setOf("prepare", "apply"),
		Outputs:    setOf("path", "artifacts"),
	},
	"InstallArtifacts": {Kind: "InstallArtifacts", SchemaFile: "install-artifacts.schema.json", Roles: setOf("apply")},
	"InstallPackages":  {Kind: "InstallPackages", SchemaFile: "install-packages.schema.json", Roles: setOf("apply")},
	"EditFile":         {Kind: "EditFile", SchemaFile: "edit-file.schema.json", Roles: setOf("apply")},
	"CopyFile":         {Kind: "CopyFile", SchemaFile: "copy-file.schema.json", Roles: setOf("apply"), Outputs: setOf("dest")},
	"Sysctl":           {Kind: "Sysctl", SchemaFile: "sysctl.schema.json", Roles: setOf("apply")},
	"Service":          {Kind: "Service", SchemaFile: "service.schema.json", Roles: setOf("apply"), Outputs: setOf("name")},
	"EnsureDir":        {Kind: "EnsureDir", SchemaFile: "ensure-dir.schema.json", Roles: setOf("apply"), Outputs: setOf("path")},
	"Symlink":          {Kind: "Symlink", SchemaFile: "symlink.schema.json", Roles: setOf("apply"), Outputs: setOf("path")},
	"InstallFile":      {Kind: "InstallFile", SchemaFile: "install-file.schema.json", Roles: setOf("apply"), Outputs: setOf("path")},
	"SystemdUnit":      {Kind: "SystemdUnit", SchemaFile: "systemd-unit.schema.json", Roles: setOf("apply"), Outputs: setOf("path")},
	"RepoConfig":       {Kind: "RepoConfig", SchemaFile: "repo-config.schema.json", Roles: setOf("apply"), Outputs: setOf("path")},
	"PackageCache":     {Kind: "PackageCache", SchemaFile: "package-cache.schema.json", Roles: setOf("apply")},
	"ContainerdConfig": {Kind: "ContainerdConfig", SchemaFile: "containerd-config.schema.json", Roles: setOf("apply"), Outputs: setOf("path")},
	"Swap":             {Kind: "Swap", SchemaFile: "swap.schema.json", Roles: setOf("apply")},
	"KernelModule":     {Kind: "KernelModule", SchemaFile: "kernel-module.schema.json", Roles: setOf("apply"), Outputs: setOf("name", "names")},
	"RunCommand":       {Kind: "RunCommand", SchemaFile: "run-command.schema.json", Roles: setOf("apply")},
	"WaitPath":         {Kind: "WaitPath", SchemaFile: "wait-path.schema.json", Roles: setOf("apply")},
	"VerifyImages":     {Kind: "VerifyImages", SchemaFile: "verify-images.schema.json", Roles: setOf("apply")},
	"KubeadmInit":      {Kind: "KubeadmInit", SchemaFile: "kubeadm-init.schema.json", Roles: setOf("apply"), Outputs: setOf("joinFile")},
	"KubeadmJoin":      {Kind: "KubeadmJoin", SchemaFile: "kubeadm-join.schema.json", Roles: setOf("apply")},
	"KubeadmReset":     {Kind: "KubeadmReset", SchemaFile: "kubeadm-reset.schema.json", Roles: setOf("apply")},
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

func StepHasOutput(kind, output string) bool {
	contract, ok := stepContracts[kind]
	if !ok {
		return false
	}
	return contract.Outputs[output]
}

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
