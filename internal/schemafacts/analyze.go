package schemafacts

import (
	"fmt"
	"sort"
	"strings"
)

func Analyze(schema map[string]any) DocumentFacts {
	fields := CollectFields(schema)
	return DocumentFacts{
		Fields:        dedupeFields(fields),
		RuleSummaries: ExtractRules(schema, ""),
	}
}

func CollectFields(schema map[string]any) []FieldFact {
	var fields []FieldFact
	collectFields(schema, schema, "", RequirementOptional, &fields)
	return dedupeFields(fields)
}

func RequirementMap(schema map[string]any, prefix string) map[string]RequirementLevel {
	facts := Analyze(schema)
	requirements := map[string]RequirementLevel{}
	for _, field := range facts.Fields {
		if strings.TrimSpace(prefix) != "" && !strings.HasPrefix(field.Path, prefix) {
			continue
		}
		requirements[field.Path] = field.Requirement
	}
	return requirements
}

func ExtractRules(node map[string]any, prefix string) []string {
	var rules []string
	if anyOf, ok := node["anyOf"].([]any); ok {
		var requiredGroups []string
		for _, raw := range anyOf {
			entry, _ := raw.(map[string]any)
			req := toStrings(entry["required"])
			if len(req) > 0 {
				requiredGroups = append(requiredGroups, strings.Join(req, ", "))
			}
		}
		if len(requiredGroups) > 0 && prefix == "" {
			groups := make([]string, 0, len(requiredGroups))
			for _, group := range requiredGroups {
				groups = append(groups, "`"+group+"`")
			}
			rules = append(rules, "At least one of the top-level groups "+joinWithFinalConjunction(groups, "or")+" must be present.")
		} else if len(requiredGroups) > 0 {
			groups := make([]string, 0, len(requiredGroups))
			for _, group := range requiredGroups {
				groups = append(groups, prefixDotJoin(prefix, strings.Split(group, ", ")))
			}
			rules = append(rules, "At least one of "+joinWithFinalConjunction(groups, "or")+" must be set.")
		}
		for _, raw := range anyOf {
			entry, _ := raw.(map[string]any)
			if strings.Contains(fmt.Sprint(entry), "const") {
				rules = append(rules, "At least one of the listed branches must match.")
				break
			}
		}
	}
	if oneOf, ok := node["oneOf"].([]any); ok && len(oneOf) > 0 {
		var choiceFields []string
		for _, raw := range oneOf {
			entry, _ := raw.(map[string]any)
			req := toStrings(entry["required"])
			notReq := toStrings(mapValue(entry, "not", "required"))
			if len(req) == 1 && len(notReq) == 1 {
				rules = append(rules, fmt.Sprintf("Exactly one of %s or %s must be set.", prefixPath(prefix, req[0]), prefixPath(prefix, notReq[0])))
				break
			}
			if len(req) == 1 {
				choiceFields = append(choiceFields, prefixPath(prefix, req[0]))
			}
		}
		if len(choiceFields) > 1 {
			rules = append(rules, fmt.Sprintf("Exactly one of %s must be set.", joinWithFinalConjunction(choiceFields, "or")))
		}
	}
	if allOf, ok := node["allOf"].([]any); ok {
		for _, raw := range allOf {
			entry, _ := raw.(map[string]any)
			ifNode, _ := entry["if"].(map[string]any)
			thenNode, _ := entry["then"].(map[string]any)
			action := stringValue(mapValue(ifNode, "properties", "action", "const"))
			if action == "" {
				enumAction := toStrings(mapValue(ifNode, "properties", "action", "enum"))
				if len(enumAction) > 0 {
					required := toStrings(thenNode["required"])
					if len(required) > 0 {
						rules = append(rules, fmt.Sprintf("When `%s.action` is one of `%s`, %s are required.", prefix, strings.Join(enumAction, "`, `"), prefixDotJoin(prefix, required)))
					}
				}
				continue
			}
			required := toStrings(thenNode["required"])
			if len(required) > 0 {
				rules = append(rules, fmt.Sprintf("When `%s.action=%s`, %s are required.", prefix, action, prefixDotJoin(prefix, required)))
			}
			if anyOf, ok := thenNode["anyOf"].([]any); ok && len(anyOf) > 0 {
				var choices []string
				for _, choiceRaw := range anyOf {
					choice, _ := choiceRaw.(map[string]any)
					choiceReq := toStrings(choice["required"])
					for _, item := range choiceReq {
						choices = append(choices, fmt.Sprintf("`%s.%s`", prefix, item))
					}
				}
				if len(choices) > 0 {
					rules = append(rules, fmt.Sprintf("When `%s.action=%s`, at least one of %s must be set.", prefix, action, strings.Join(choices, " or ")))
				}
			}
		}
	}
	if notNode, ok := node["not"].(map[string]any); ok {
		if allOf, ok := notNode["allOf"].([]any); ok && len(allOf) > 0 && prefix == "" {
			rules = append(rules, "Top-level `phases` and top-level `steps` cannot both be set in the same workflow.")
		}
	}
	return dedupeStrings(rules)
}

func collectFields(root, node map[string]any, prefix string, inherited RequirementLevel, fields *[]FieldFact) {
	node = resolveSchemaNode(root, node)
	props, _ := node["properties"].(map[string]any)
	if len(props) == 0 {
		return
	}
	directReq := requiredSet(node["required"])
	conditionalReq := conditionalRequiredSet(node)
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		child, ok := props[key].(map[string]any)
		if !ok {
			continue
		}
		child = resolveSchemaNode(root, child)
		path := joinPath(prefix, key)
		requirement := inherited
		switch {
		case inherited == RequirementConditional:
			requirement = RequirementConditional
		case directReq[key]:
			requirement = RequirementRequired
		case conditionalReq[key] && requirement != RequirementRequired:
			requirement = RequirementConditional
		case requirement == "":
			requirement = RequirementOptional
		}
		field := FieldFact{
			Path:        path,
			Type:        fieldType(root, child),
			Required:    directReq[key],
			Requirement: requirement,
			Default:     valueString(child["default"]),
			Enum:        toStrings(child["enum"]),
			Description: stringValue(child["description"]),
			Example:     inferExample(root, child),
		}
		*fields = append(*fields, field)
		nextInherited := RequirementOptional
		if requirement == RequirementConditional {
			nextInherited = RequirementConditional
		}
		if field.Type == "object" {
			collectFields(root, child, path, nextInherited, fields)
		}
		items, _ := resolveSchemaNode(root, child)["items"].(map[string]any)
		items = resolveSchemaNode(root, items)
		if field.Type == "array<object>" && len(items) > 0 {
			collectFields(root, items, path+"[]", nextInherited, fields)
		}
	}
}

func conditionalRequiredSet(node map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, groupKey := range []string{"anyOf", "oneOf"} {
		for _, raw := range toAnySlice(node[groupKey]) {
			entry, _ := raw.(map[string]any)
			for _, field := range toStrings(entry["required"]) {
				out[field] = true
			}
		}
	}
	if allOf, ok := node["allOf"].([]any); ok {
		for _, raw := range allOf {
			entry, _ := raw.(map[string]any)
			thenNode, _ := entry["then"].(map[string]any)
			for _, field := range toStrings(thenNode["required"]) {
				out[field] = true
			}
			for _, anyRaw := range toAnySlice(thenNode["anyOf"]) {
				choice, _ := anyRaw.(map[string]any)
				for _, field := range toStrings(choice["required"]) {
					out[field] = true
				}
			}
		}
	}
	return out
}

func dedupeFields(fields []FieldFact) []FieldFact {
	seen := map[string]bool{}
	var out []FieldFact
	for _, field := range fields {
		if seen[field.Path] {
			continue
		}
		seen[field.Path] = true
		out = append(out, field)
	}
	return out
}

func resolveSchemaNode(root, node map[string]any) map[string]any {
	if node == nil {
		return nil
	}
	ref, _ := node["$ref"].(string)
	if ref == "" || !strings.HasPrefix(ref, "#/") {
		return node
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	var current any = root
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return node
		}
		current = m[part]
	}
	resolved, ok := current.(map[string]any)
	if !ok {
		return node
	}
	return resolved
}

func requiredSet(raw any) map[string]bool {
	set := map[string]bool{}
	for _, item := range toStrings(raw) {
		set[item] = true
	}
	return set
}

func fieldType(root, node map[string]any) string {
	node = resolveSchemaNode(root, node)
	if t, _ := node["type"].(string); t != "" {
		if t == "array" {
			items, _ := node["items"].(map[string]any)
			items = resolveSchemaNode(root, items)
			if itemType, _ := items["type"].(string); itemType != "" {
				return "array<" + itemType + ">"
			}
			if len(items) > 0 {
				return "array<object>"
			}
		}
		return t
	}
	if node["const"] != nil {
		return "string"
	}
	if node["properties"] != nil {
		return "object"
	}
	return "unknown"
}

func inferExample(root, node map[string]any) string {
	node = resolveSchemaNode(root, node)
	if value := node["const"]; value != nil {
		return valueString(value)
	}
	if value := node["default"]; value != nil {
		return valueString(value)
	}
	if values := toStrings(node["enum"]); len(values) > 0 {
		return values[0]
	}
	switch fieldType(root, node) {
	case "string":
		return "example"
	case "integer", "number":
		return "1"
	case "boolean":
		return "true"
	case "array<string>":
		return "[example]"
	case "array<object>":
		return "[{...}]"
	case "object":
		return "{...}"
	default:
		return ""
	}
}

func FilterDirectChildFields(fields []FieldFact, prefix string) []FieldFact {
	var out []FieldFact
	for _, f := range fields {
		if prefix == "" {
			if !strings.Contains(f.Path, ".") && !strings.Contains(f.Path, "[]") {
				out = append(out, f)
			}
			continue
		}
		if !strings.HasPrefix(f.Path, prefix+".") && !strings.HasPrefix(f.Path, prefix+"[]") {
			continue
		}
		remainder := strings.TrimPrefix(strings.TrimPrefix(f.Path, prefix+"."), prefix+"[]")
		if !strings.Contains(remainder, ".") && !strings.Contains(remainder, "[]") {
			out = append(out, f)
		}
	}
	return out
}

func NestedSectionPaths(fields []FieldFact, prefix string) []string {
	seen := map[string]bool{}
	var out []string
	for _, field := range fields {
		if field.Type != "object" && field.Type != "array<object>" {
			continue
		}
		if field.Path == "" || !strings.Contains(field.Path, ".") {
			if prefix == "" || field.Path == prefix {
				continue
			}
		}
		if prefix != "" && !strings.HasPrefix(field.Path, prefix+".") {
			continue
		}
		if seen[field.Path] {
			continue
		}
		seen[field.Path] = true
		out = append(out, field.Path)
	}
	sort.Strings(out)
	return out
}

func RenderRuleSummaries(rules []string) []string {
	return dedupeStrings(rules)
}

func joinWithFinalConjunction(values []string, conjunction string) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}
	if len(values) == 2 {
		return values[0] + " " + conjunction + " " + values[1]
	}
	return strings.Join(values[:len(values)-1], ", ") + ", " + conjunction + " " + values[len(values)-1]
}

func valueString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func toStrings(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toAnySlice(v any) []any { items, _ := v.([]any); return items }

func mapValue(root any, path ...string) any {
	current, _ := root.(map[string]any)
	for _, key := range path {
		if current == nil {
			return nil
		}
		next := current[key]
		if m, ok := next.(map[string]any); ok {
			current = m
			continue
		}
		if key == path[len(path)-1] {
			return next
		}
		return nil
	}
	return current
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func prefixDotJoin(prefix string, items []string) string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, prefixPath(prefix, item))
	}
	return strings.Join(out, ", ")
}

func prefixPath(prefix, item string) string {
	if prefix == "" {
		return fmt.Sprintf("`%s`", item)
	}
	return fmt.Sprintf("`%s.%s`", prefix, item)
}

func dedupeStrings(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
