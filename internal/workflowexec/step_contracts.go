package workflowexec

type StepContract struct {
	SchemaFile string
	Roles      map[string]bool
	Outputs    map[string]bool
}

func stepContracts() map[string]StepContract {
	contracts := make(map[string]StepContract, len(StepDefinitions()))
	for _, def := range StepDefinitions() {
		contracts[def.Kind] = StepContract{
			SchemaFile: def.SchemaFile,
			Roles:      setOf(def.Roles...),
			Outputs:    setOf(def.Outputs...),
		}
	}
	return contracts
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
	defs := StepDefinitions()
	kinds := make([]string, 0, len(defs))
	for _, def := range defs {
		kinds = append(kinds, def.Kind)
	}
	return kinds
}

func StepAllowedForRole(role, kind string) bool {
	contract, ok := stepContracts()[kind]
	if !ok {
		return false
	}
	return contract.Roles[role]
}

func StepHasOutput(kind, output string) bool {
	contract, ok := stepContracts()[kind]
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
