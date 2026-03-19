package workflowexec

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
	contracts := make(map[string]StepContract, len(StepDefinitions()))
	for _, def := range StepDefinitions() {
		actions := map[string]ActionContract{}
		for _, action := range def.Actions {
			actions[action.Name] = ActionContract{
				Outputs: setOf(action.Outputs...),
				Roles:   setOf(action.Roles...),
			}
		}
		contracts[def.Kind] = StepContract{
			SchemaFile: def.SchemaFile,
			Roles:      setOf(def.Roles...),
			Outputs:    setOf(def.Outputs...),
			Actions:    actions,
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

func StepAllowedForRole(role, kind string, spec map[string]any) bool {
	contract, ok := stepContracts()[kind]
	if !ok {
		return false
	}
	action := inferContractAction(kind, spec)
	if len(contract.Actions) > 0 && action == "" {
		return contract.Roles[role]
	}
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
	decoded, err := DecodeSpec[struct {
		Action string `json:"action"`
	}](spec)
	if err == nil && decoded.Action != "" {
		return decoded.Action
	}
	return ""
}

func setOf(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}
