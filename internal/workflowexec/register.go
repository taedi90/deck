package workflowexec

import (
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/config"
)

func ApplyRegister(step config.Step, outputs map[string]any, runtimeVars map[string]any, missingErrCode string) error {
	if len(step.Register) == 0 {
		return nil
	}
	for runtimeKey, outputKey := range step.Register {
		if IsReservedRuntimeVar(runtimeKey) {
			return fmt.Errorf("E_RUNTIME_VAR_RESERVED: %s", runtimeKey)
		}
		v, ok := outputs[outputKey]
		if !ok {
			return fmt.Errorf("%s: step %s kind %s has no output key %s", missingErrCode, step.ID, step.Kind, outputKey)
		}
		runtimeVars[runtimeKey] = v
	}
	return nil
}

func IsReservedRuntimeVar(runtimeKey string) bool {
	trimmed := strings.TrimSpace(runtimeKey)
	return trimmed == "host" || strings.HasPrefix(trimmed, "host.")
}
