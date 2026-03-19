package install

import "github.com/taedi90/deck/internal/workflowexec"

func evaluateWhen(expr string, vars map[string]any, runtime map[string]any) (bool, error) {
	return workflowexec.EvaluateWhen(expr, vars, runtime, errCodeConditionEval)
}

func EvaluateWhen(expr string, vars map[string]any, runtime map[string]any) (bool, error) {
	return evaluateWhen(expr, vars, runtime)
}
