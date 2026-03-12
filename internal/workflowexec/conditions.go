package workflowexec

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func EvaluateWhen(expr string, vars map[string]any, runtime map[string]any, ctx map[string]any, errCode string) (bool, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return true, nil
	}

	tokens, err := tokenizeCondition(trimmed)
	if err != nil {
		return false, fmt.Errorf("%s: %w", errCode, err)
	}
	p := &condParser{tokens: tokens, vars: vars, runtime: runtime, ctx: ctx}
	value, err := p.parseExpr()
	if err != nil {
		return false, fmt.Errorf("%s: %w", errCode, err)
	}
	if p.hasNext() {
		return false, fmt.Errorf("%s: unexpected token %q", errCode, p.peek().value)
	}
	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s: condition must evaluate to boolean", errCode)
	}
	return b, nil
}

type condToken struct {
	kind  string
	value string
}

type condParser struct {
	tokens  []condToken
	pos     int
	vars    map[string]any
	runtime map[string]any
	ctx     map[string]any
}

func tokenizeCondition(expr string) ([]condToken, error) {
	tokens := make([]condToken, 0)
	for i := 0; i < len(expr); {
		ch := expr[i]
		if ch == ' ' || ch == '\t' || ch == '\n' {
			i++
			continue
		}
		if ch == '(' || ch == ')' {
			tokens = append(tokens, condToken{kind: string(ch), value: string(ch)})
			i++
			continue
		}
		if i+1 < len(expr) {
			two := expr[i : i+2]
			if two == "==" || two == "!=" {
				tokens = append(tokens, condToken{kind: two, value: two})
				i += 2
				continue
			}
		}
		if ch == '"' {
			j := i + 1
			for j < len(expr) && expr[j] != '"' {
				if expr[j] == '\\' && j+1 < len(expr) {
					j += 2
					continue
				}
				j++
			}
			if j >= len(expr) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			raw := expr[i+1 : j]
			unquoted, err := strconv.Unquote("\"" + strings.ReplaceAll(raw, "\"", "\\\"") + "\"")
			if err != nil {
				return nil, fmt.Errorf("invalid string literal")
			}
			tokens = append(tokens, condToken{kind: "string", value: unquoted})
			i = j + 1
			continue
		}
		if isIdentStart(ch) {
			j := i + 1
			for j < len(expr) && isIdentPart(expr[j]) {
				j++
			}
			word := expr[i:j]
			tokens = append(tokens, condToken{kind: "ident", value: word})
			i = j
			continue
		}
		return nil, fmt.Errorf("invalid character %q", ch)
	}
	return tokens, nil
}

func (p *condParser) parseExpr() (any, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("or") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of or is not boolean")
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of or is not boolean")
		}
		left = lb || rb
	}
	return left, nil
}

func (p *condParser) parseAnd() (any, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.matchIdent("and") {
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of and is not boolean")
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of and is not boolean")
		}
		left = lb && rb
	}
	return left, nil
}

func (p *condParser) parseUnary() (any, error) {
	if p.matchIdent("not") {
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("operand of not is not boolean")
		}
		return !b, nil
	}
	return p.parsePrimary()
}

func (p *condParser) parsePrimary() (any, error) {
	if p.matchKind("(") {
		v, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.matchKind(")") {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		return v, nil
	}

	left, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	if p.matchKind("==") {
		right, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return compareValues(left, right), nil
	}
	if p.matchKind("!=") {
		right, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		return !compareValues(left, right), nil
	}
	return left, nil
}

func (p *condParser) parseValue() (any, error) {
	if !p.hasNext() {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	tok := p.next()
	if tok.kind == "string" {
		return tok.value, nil
	}
	if tok.kind == "ident" {
		if tok.value == "true" {
			return true, nil
		}
		if tok.value == "false" {
			return false, nil
		}
		if v, ok := p.resolveIdentifier(tok.value); ok {
			return v, nil
		}
		return nil, unknownIdentifierError(tok.value)
	}
	return nil, fmt.Errorf("unexpected token %q", tok.value)
}

func (p *condParser) resolveIdentifier(id string) (any, bool) {
	if strings.HasPrefix(id, "vars.") {
		return resolveNestedMap(p.vars, strings.TrimPrefix(id, "vars."))
	}
	if strings.HasPrefix(id, "runtime.") {
		return resolveNestedMap(p.runtime, strings.TrimPrefix(id, "runtime."))
	}
	return nil, false
}

func unknownIdentifierError(id string) error {
	if strings.Contains(id, ".") {
		return fmt.Errorf("unknown identifier %q; supported prefixes are vars. and runtime", id)
	}
	return fmt.Errorf("unknown identifier %q; use vars.%s", id, id)
}

func resolveNestedMap(root map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, false
	}
	cur := any(root)
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

func (p *condParser) hasNext() bool {
	return p.pos < len(p.tokens)
}

func (p *condParser) peek() condToken {
	return p.tokens[p.pos]
}

func (p *condParser) next() condToken {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (p *condParser) matchKind(kind string) bool {
	if !p.hasNext() {
		return false
	}
	if p.peek().kind != kind {
		return false
	}
	p.pos++
	return true
}

func (p *condParser) matchIdent(word string) bool {
	if !p.hasNext() {
		return false
	}
	tok := p.peek()
	if tok.kind != "ident" || tok.value != word {
		return false
	}
	p.pos++
	return true
}

func compareValues(a, b any) bool {
	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int:
		bf, ok := numberAsFloat64(b)
		return ok && float64(av) == bf
	case int64:
		bf, ok := numberAsFloat64(b)
		return ok && float64(av) == bf
	case float64:
		bf, ok := numberAsFloat64(b)
		return ok && math.Abs(av-bf) < 1e-9
	default:
		return fmt.Sprint(a) == fmt.Sprint(b)
	}
}

func numberAsFloat64(v any) (float64, bool) {
	switch nv := v.(type) {
	case int:
		return float64(nv), true
	case int64:
		return float64(nv), true
	case float64:
		return nv, true
	default:
		return 0, false
	}
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9') || ch == '.'
}
