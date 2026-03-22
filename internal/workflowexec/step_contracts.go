package workflowexec

import "strings"

func normalizeStepKey(key StepTypeKey) StepTypeKey {
	return StepTypeKey{APIVersion: strings.TrimSpace(key.APIVersion), Kind: strings.TrimSpace(key.Kind)}
}

func StepSchemaFileForKey(key StepTypeKey) (string, bool) {
	def, ok := BuiltInTypeDefinitionForKey(normalizeStepKey(key))
	if !ok || def.Step.SchemaFile == "" {
		return "", false
	}
	return def.Step.SchemaFile, true
}

func StepKinds() []string {
	defs := StepDefinitions()
	kinds := make([]string, 0, len(defs))
	for _, def := range defs {
		kinds = append(kinds, def.Kind)
	}
	return kinds
}

func StepAllowedForRoleForKey(role string, key StepTypeKey) bool {
	def, ok := BuiltInTypeDefinitionForKey(normalizeStepKey(key))
	if !ok {
		return false
	}
	return containsString(def.Step.Roles, role)
}

func StepHasOutputForKey(key StepTypeKey, output string) bool {
	def, ok := BuiltInTypeDefinitionForKey(normalizeStepKey(key))
	if !ok {
		return false
	}
	return containsString(def.Step.Outputs, output)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
