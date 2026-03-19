package install

import (
	"context"
	"strings"
	"testing"
)

func nilContextForPackagesTest() context.Context { return nil }

func TestRunInstallPackages_RejectsNilContext(t *testing.T) {
	err := runInstallPackages(nilContextForPackagesTest(), map[string]any{"packages": []any{"containerd"}})
	if err == nil {
		t.Fatalf("expected nil context error")
	}
	if !strings.Contains(err.Error(), "context is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}
