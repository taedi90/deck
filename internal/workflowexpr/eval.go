package workflowexpr

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"

	"github.com/Airgap-Castaways/deck/internal/workflowcontract"
)

type Inputs struct {
	Vars    map[string]any
	Runtime map[string]any
}

type Program struct {
	program cel.Program
}

var (
	envOnce sync.Once
	envInst *cel.Env
	errEnv  error

	identPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_\.]*`)
)

func CompileWhen(expr string) (Program, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return Program{}, nil
	}
	if err := validateIdentifiers(trimmed); err != nil {
		return Program{}, err
	}
	env, err := env()
	if err != nil {
		return Program{}, err
	}
	ast, issues := env.Compile(trimmed)
	if issues != nil && issues.Err() != nil {
		return Program{}, augmentCompileError(trimmed, issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		return Program{}, fmt.Errorf("compile CEL program: %w", err)
	}
	return Program{program: program}, nil
}

func EvaluateWhen(expr string, in Inputs) (bool, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return true, nil
	}
	program, err := CompileWhen(trimmed)
	if err != nil {
		return false, err
	}
	return program.Eval(in)
}

func (p Program) Eval(in Inputs) (bool, error) {
	if p.program == nil {
		return true, nil
	}
	out, _, err := p.program.Eval(map[string]any{
		"vars":    cloneMap(in.Vars),
		"runtime": cloneMap(in.Runtime),
	})
	if err != nil {
		return false, fmt.Errorf("evaluate CEL expression: %w", err)
	}
	if out == types.True {
		return true, nil
	}
	if out == types.False {
		return false, nil
	}
	if value, ok := out.Value().(bool); ok {
		return value, nil
	}
	return false, fmt.Errorf("condition must evaluate to boolean")
}

func env() (*cel.Env, error) {
	envOnce.Do(func() {
		envInst, errEnv = cel.NewEnv(
			cel.Variable("vars", cel.MapType(cel.StringType, cel.DynType)),
			cel.Variable("runtime", cel.MapType(cel.StringType, cel.DynType)),
		)
	})
	return envInst, errEnv
}

func validateIdentifiers(expr string) error {
	for _, ident := range scanIdentifiers(expr) {
		if ident == "true" || ident == "false" || ident == "null" {
			continue
		}
		switch ident {
		case "and":
			return fmt.Errorf("compile CEL expression: legacy boolean operator %q is not supported (use && instead of and)", ident)
		case "or":
			return fmt.Errorf("compile CEL expression: legacy boolean operator %q is not supported (use || instead of or)", ident)
		case "not":
			return fmt.Errorf("compile CEL expression: legacy boolean operator %q is not supported (use ! instead of not)", ident)
		}
		root := ident
		if dot := strings.Index(root, "."); dot >= 0 {
			root = root[:dot]
		}
		if workflowcontract.IsPublicWhenNamespace(root) {
			continue
		}
		return unknownIdentifierError(ident)
	}
	return nil
}

func scanIdentifiers(expr string) []string {
	masked := maskStringLiterals(expr)

	matches := identPattern.FindAllString(masked, -1)
	idents := make([]string, 0, len(matches))
	for _, ident := range matches {
		if ident == "" {
			continue
		}
		idents = append(idents, ident)
	}
	return idents
}

func maskStringLiterals(expr string) string {
	var b strings.Builder
	b.Grow(len(expr))
	inString := byte(0)
	escaped := false
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if inString != 0 {
			if escaped {
				escaped = false
				b.WriteByte(' ')
				continue
			}
			if ch == '\\' {
				escaped = true
				b.WriteByte(' ')
				continue
			}
			if ch == inString {
				inString = 0
			}
			b.WriteByte(' ')
			continue
		}
		if ch == '\'' || ch == '"' {
			inString = ch
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func unknownIdentifierError(id string) error {
	if strings.Contains(id, ".") {
		return fmt.Errorf("unknown identifier %q; supported prefixes are vars. and runtime", id)
	}
	return fmt.Errorf("unknown identifier %q; use vars.%s", id, id)
}

func augmentCompileError(expr string, err error) error {
	message := err.Error()
	hints := make([]string, 0, 3)
	if legacyWordPresent(expr, "and") {
		hints = append(hints, "use && instead of and")
	}
	if legacyWordPresent(expr, "or") {
		hints = append(hints, "use || instead of or")
	}
	if legacyWordPresent(expr, "not") {
		hints = append(hints, "use ! instead of not")
	}
	if len(hints) == 0 {
		return fmt.Errorf("compile CEL expression: %w", err)
	}
	return fmt.Errorf("compile CEL expression: %s (%s)", message, strings.Join(hints, "; "))
}

func legacyWordPresent(expr, word string) bool {
	masked := maskStringLiterals(expr)
	for _, ident := range identPattern.FindAllString(masked, -1) {
		if ident == word {
			return true
		}
	}
	return false
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
