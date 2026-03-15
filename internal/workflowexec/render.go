package workflowexec

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/taedi90/deck/internal/config"
)

func RenderSpec(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any, ctxData map[string]any) (map[string]any, error) {
	if spec == nil {
		return map[string]any{}, nil
	}
	vars := map[string]any{}
	if wf != nil && wf.Vars != nil {
		vars = wf.Vars
	}
	ctx := map[string]any{
		"vars":    vars,
		"context": cloneMap(ctxData),
		"runtime": runtimeVars,
	}
	if ctx["context"] == nil {
		ctx["context"] = map[string]any{}
	}
	return renderMap(spec, ctx)
}

func RenderSpecWithExtra(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any, ctxData map[string]any, extra map[string]any) (map[string]any, error) {
	vars := map[string]any{}
	if wf != nil && wf.Vars != nil {
		vars = wf.Vars
	}
	ctx := map[string]any{
		"vars":    vars,
		"context": cloneMap(ctxData),
		"runtime": runtimeVars,
	}
	if ctx["context"] == nil {
		ctx["context"] = map[string]any{}
	}
	for key, value := range extra {
		ctx[key] = value
	}
	if spec == nil {
		return map[string]any{}, nil
	}
	return renderMap(spec, ctx)
}

func renderMap(input map[string]any, ctx map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(input))
	for k, v := range input {
		rendered, err := renderAny(v, ctx)
		if err != nil {
			return nil, fmt.Errorf("spec.%s: %w", k, err)
		}
		out[k] = rendered
	}
	return out, nil
}

func renderAny(v any, ctx map[string]any) (any, error) {
	switch tv := v.(type) {
	case string:
		return renderString(tv, ctx)
	case map[string]any:
		return renderMap(tv, ctx)
	case []any:
		out := make([]any, 0, len(tv))
		for idx, item := range tv {
			rendered, err := renderAny(item, ctx)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", idx, err)
			}
			out = append(out, rendered)
		}
		return out, nil
	default:
		return v, nil
	}
}

func renderString(input string, ctx map[string]any) (string, error) {
	tmpl, err := template.New("spec").Option("missingkey=error").Parse(input)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, ctx); err != nil {
		return "", err
	}
	return out.String(), nil
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
