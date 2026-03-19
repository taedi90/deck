package workflowexec

import (
	"fmt"

	"github.com/taedi90/deck/internal/workflowexpr"
)

func EvaluateWhen(expr string, vars map[string]any, runtime map[string]any, errCode string) (bool, error) {
	result, err := workflowexpr.EvaluateWhen(expr, workflowexpr.Inputs{Vars: vars, Runtime: runtime})
	if err != nil {
		return false, fmt.Errorf("%s: %w", errCode, err)
	}
	return result, nil
}
