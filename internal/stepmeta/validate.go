package stepmeta

import (
	"fmt"
	"strings"
)

func validateEntry(entry Entry) error {
	if strings.TrimSpace(entry.Definition.Visibility) != "public" {
		return nil
	}
	problems := make([]string, 0)
	if strings.TrimSpace(entry.Docs.Summary) == "" {
		problems = append(problems, formatSourceProblem(entry.Docs.Source, "missing type summary"))
	}
	if strings.TrimSpace(entry.Docs.WhenToUse) == "" {
		problems = append(problems, formatSourceProblem(entry.Docs.Source, "missing @deck.when"))
	}
	if strings.TrimSpace(entry.Docs.Example) == "" {
		problems = append(problems, formatSourceProblem(entry.Docs.Source, "missing @deck.example"))
	}
	for _, field := range entry.Docs.Fields {
		if strings.TrimSpace(field.Description) == "" {
			problems = append(problems, formatSourceProblem(field.Source, fmt.Sprintf("missing description for %s", field.Path)))
		}
		if requiresExample(field) && strings.TrimSpace(field.Example) == "" {
			problems = append(problems, formatSourceProblem(field.Source, fmt.Sprintf("missing example for %s", field.Path)))
		}
		if isPlaceholder(field.Example) {
			problems = append(problems, formatSourceProblem(field.Source, fmt.Sprintf("placeholder example for %s", field.Path)))
		}
	}
	if isPlaceholder(entry.Docs.Example) {
		problems = append(problems, formatSourceProblem(entry.Docs.Source, "placeholder type example"))
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("stepmeta: %s (%s): %s", entry.Definition.Kind, entry.TypeName, strings.Join(problems, "; "))
}

func formatSourceProblem(source SourceRef, message string) string {
	if strings.TrimSpace(source.File) == "" || source.Line <= 0 {
		return message
	}
	return fmt.Sprintf("%s:%d %s", source.File, source.Line, message)
}

func requiresExample(field FieldDoc) bool {
	if field.Required {
		return true
	}
	path := strings.ToLower(strings.TrimSpace(field.Path))
	for _, needle := range []string{"path", "timeout", "template", "mode", "url", "bundle", "source", "fetch"} {
		if strings.Contains(path, needle) {
			return true
		}
	}
	return false
}

func isPlaceholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return trimmed == "example" || trimmed == "spec: {}"
}
