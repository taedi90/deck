package workflowexec

import (
	"bytes"
	"fmt"
	"reflect"
	"text/template"
	"text/template/parse"

	"github.com/Airgap-Castaways/deck/internal/cloneutil"
	"github.com/Airgap-Castaways/deck/internal/config"
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
		"context": cloneutil.DeepMap(ctxData),
		"runtime": runtimeVars,
	}
	if ctx["context"] == nil {
		ctx["context"] = map[string]any{}
	}
	return renderMap(spec, ctx)
}

func ResolveWholeValueTemplate(input string, ctx map[string]any) (any, bool, error) {
	return renderWholeValue(input, ctx)
}

func RenderSpecWithExtra(spec map[string]any, wf *config.Workflow, runtimeVars map[string]any, ctxData map[string]any, extra map[string]any) (map[string]any, error) {
	vars := map[string]any{}
	if wf != nil && wf.Vars != nil {
		vars = wf.Vars
	}
	ctx := map[string]any{
		"vars":    vars,
		"context": cloneutil.DeepMap(ctxData),
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
		return renderStringOrValue(tv, ctx)
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
		return cloneutil.DeepValue(v), nil
	}
}

func renderStringOrValue(input string, ctx map[string]any) (any, error) {
	if value, ok, err := renderWholeValue(input, ctx); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return renderInjectedValue(value, input, ctx)
	}
	return renderString(input, ctx)
}

func renderInjectedValue(value any, original string, ctx map[string]any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return renderMap(cloneutil.DeepMap(typed), ctx)
	case []any:
		out := make([]any, 0, len(typed))
		for idx, item := range typed {
			rendered, err := renderAny(cloneutil.DeepValue(item), ctx)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", idx, err)
			}
			out = append(out, rendered)
		}
		return out, nil
	case string:
		if typed == original {
			return renderString(typed, ctx)
		}
		return renderStringOrValue(typed, ctx)
	default:
		return typed, nil
	}
}

func renderWholeValue(input string, ctx map[string]any) (any, bool, error) {
	tmpl, err := template.New("spec-value").Option("missingkey=error").Parse(input)
	if err != nil {
		return nil, false, err
	}
	if tmpl.Tree == nil || tmpl.Root == nil || len(tmpl.Root.Nodes) != 1 {
		return nil, false, nil
	}
	action, ok := tmpl.Root.Nodes[0].(*parse.ActionNode)
	if !ok {
		return nil, false, nil
	}
	value, ok, err := evalActionNode(action, ctx)
	if err != nil {
		return nil, true, err
	}
	if !ok {
		return nil, false, nil
	}
	return cloneutil.DeepValue(value), true, nil
}

func evalActionNode(action *parse.ActionNode, ctx map[string]any) (any, bool, error) {
	if action == nil || action.Pipe == nil {
		return nil, false, nil
	}
	if len(action.Pipe.Decl) > 0 || len(action.Pipe.Cmds) != 1 {
		return nil, false, nil
	}
	cmd := action.Pipe.Cmds[0]
	if cmd == nil || len(cmd.Args) == 0 {
		return nil, false, nil
	}
	if ident, ok := cmd.Args[0].(*parse.IdentifierNode); ok && ident.Ident == "index" {
		return evalIndexCommand(cmd.Args[1:], ctx)
	}
	if len(cmd.Args) != 1 {
		return nil, false, nil
	}
	return evalArgNode(cmd.Args[0], ctx)
}

func evalIndexCommand(args []parse.Node, ctx map[string]any) (any, bool, error) {
	if len(args) < 2 {
		return nil, false, nil
	}
	current, ok, err := evalArgNode(args[0], ctx)
	if err != nil || !ok {
		return nil, ok, err
	}
	for _, arg := range args[1:] {
		selector, ok, err := evalIndexSelector(arg)
		if err != nil || !ok {
			return nil, ok, err
		}
		current, err = lookupTemplateIndexValue(current, selector)
		if err != nil {
			return nil, true, err
		}
	}
	return current, true, nil
}

func evalArgNode(node parse.Node, ctx map[string]any) (any, bool, error) {
	switch typed := node.(type) {
	case *parse.DotNode:
		return cloneutil.DeepValue(ctx), true, nil
	case *parse.FieldNode:
		value, err := resolveTemplatePath(ctx, typed.Ident)
		if err != nil {
			return nil, true, err
		}
		return value, true, nil
	case *parse.VariableNode:
		if len(typed.Ident) == 0 || typed.Ident[0] != "$" {
			return nil, false, nil
		}
		value, err := resolveTemplatePath(ctx, typed.Ident[1:])
		if err != nil {
			return nil, true, err
		}
		return value, true, nil
	default:
		return nil, false, nil
	}
}

func evalIndexSelector(node parse.Node) (any, bool, error) {
	switch typed := node.(type) {
	case *parse.StringNode:
		return typed.Text, true, nil
	case *parse.NumberNode:
		if typed.IsInt {
			return int(typed.Int64), true, nil
		}
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

func resolveTemplatePath(value any, path []string) (any, error) {
	current := value
	for _, part := range path {
		next, err := lookupTemplateValue(current, part)
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func lookupTemplateValue(current any, key string) (any, error) {
	if current == nil {
		return nil, fmt.Errorf("nil data for %q", key)
	}
	if typed, ok := current.(map[string]any); ok {
		value, exists := typed[key]
		if !exists {
			return nil, fmt.Errorf("map has no entry for key %q", key)
		}
		return value, nil
	}
	v := reflect.ValueOf(current)
	if !v.IsValid() {
		return nil, fmt.Errorf("invalid value for %q", key)
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, fmt.Errorf("nil pointer for %q", key)
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String {
		entry := v.MapIndex(reflect.ValueOf(key))
		if !entry.IsValid() {
			return nil, fmt.Errorf("map has no entry for key %q", key)
		}
		return entry.Interface(), nil
	}
	return nil, fmt.Errorf("cannot evaluate %q on %T", key, current)
}

func lookupTemplateIndexValue(current any, selector any) (any, error) {
	if key, ok := selector.(string); ok {
		return lookupTemplateValue(current, key)
	}
	idx, ok := selector.(int)
	if !ok {
		return nil, fmt.Errorf("unsupported selector type %T", selector)
	}
	if typed, ok := current.([]any); ok {
		if idx < 0 || idx >= len(typed) {
			return nil, fmt.Errorf("index %d out of range", idx)
		}
		return typed[idx], nil
	}
	v := reflect.ValueOf(current)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, fmt.Errorf("nil pointer for index %d", idx)
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil, fmt.Errorf("cannot index %T with %d", current, idx)
	}
	if idx < 0 || idx >= v.Len() {
		return nil, fmt.Errorf("index %d out of range", idx)
	}
	return v.Index(idx).Interface(), nil
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
