package validate

var workflowTopLevelModes = []string{"phases", "steps"}

const (
	workflowSupportedVersion = "v1alpha1"
	workflowImportRule       = "Imports are only supported under phases[].imports and resolve from workflows/components/."
)

func SupportedWorkflowRoles() []string {
	return []string{"prepare", "apply"}
}

func SupportedWorkflowVersion() string {
	return workflowSupportedVersion
}

func WorkflowTopLevelModes() []string {
	out := make([]string, len(workflowTopLevelModes))
	copy(out, workflowTopLevelModes)
	return out
}

func WorkflowImportRule() string {
	return workflowImportRule
}

func WorkflowInvariantNotes() []string {
	return []string{
		"A workflow must define at least one of phases or steps.",
		"A workflow cannot define both top-level phases and top-level steps at the same time.",
		workflowImportRule,
		"Workflow mode is determined by command context or file location, not by an in-file role field.",
		"Each step still validates against its own kind-specific schema after the top-level workflow schema passes.",
	}
}
