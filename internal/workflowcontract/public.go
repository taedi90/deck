package workflowcontract

import "strings"

const (
	WhenLanguage    = "CEL"
	whenDescription = "CEL expression evaluated at runtime. The step is skipped when it evaluates to false. Use `vars.` for input variables and `runtime.` for registered outputs and host facts."
	whenExample     = `vars.skipKubeadm != "true"`
	registerDesc    = "Map of runtime variable names to step output keys. Exported values are available to later steps, or to later batches when the step runs inside a parallel group, via `runtime.` in `when` expressions and `.runtime` in templates."
	registerExample = "{outputPath:path}"
)

var whenNamespaces = []string{"runtime", "vars"}

func WhenNamespaces() []string {
	out := make([]string, len(whenNamespaces))
	copy(out, whenNamespaces)
	return out
}

func IsPublicWhenNamespace(root string) bool {
	trimmed := strings.TrimSpace(root)
	for _, namespace := range whenNamespaces {
		if trimmed == namespace {
			return true
		}
	}
	return false
}

func WhenDescription() string {
	return whenDescription
}

func WhenExample() string {
	return whenExample
}

func RegisterDescription() string {
	return registerDesc
}

func RegisterExample() string {
	return registerExample
}
