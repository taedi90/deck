package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type varFlag struct {
	values map[string]string
}

func (v *varFlag) String() string {
	if v == nil || len(v.values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(v.values))
	for key, value := range v.values {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func (v *varFlag) Set(raw string) error {
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 {
		return errors.New("--var must be key=value")
	}
	key := strings.TrimSpace(parts[0])
	if key == "" {
		return errors.New("--var must be key=value")
	}
	if v.values == nil {
		v.values = map[string]string{}
	}
	v.values[key] = parts[1]
	return nil
}

func (v *varFlag) AsMap() map[string]string {
	if v == nil || len(v.values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(v.values))
	for key, value := range v.values {
		cloned[key] = value
	}
	return cloned
}

func registerFileFlags(fs *flag.FlagSet, target *string, usage string) {
	fs.StringVar(target, "file", "", usage)
	fs.StringVar(target, "f", "", usage)
}

func registerOutputFormatFlags(fs *flag.FlagSet, target *string, defaultValue string) {
	fs.StringVar(target, "output", defaultValue, "output format (text|json)")
	fs.StringVar(target, "o", defaultValue, "output format (text|json)")
}

func varsAsAnyMap(vars map[string]string) map[string]any {
	if len(vars) == 0 {
		return nil
	}
	converted := make(map[string]any, len(vars))
	for key, value := range vars {
		converted[key] = value
	}
	return converted
}

func stdoutPrintf(format string, args ...any) error {
	_, err := fmt.Fprintf(os.Stdout, format, args...)
	return err
}

func stdoutPrintln(args ...any) error {
	_, err := fmt.Fprintln(os.Stdout, args...)
	return err
}

func closeSilently(closer io.Closer) {
	_ = closer.Close()
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
