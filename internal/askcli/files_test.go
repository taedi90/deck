package askcli

import "testing"

func TestNormalizeGeneratedContentLeavesVarsUntouched(t *testing.T) {
	content := "role:\nnodeRole: control-plane\ncustomGroup:\njoinPath: /tmp/deck/join.txt\n"
	got := normalizeGeneratedContent("workflows/vars.yaml", content)
	if got != content {
		t.Fatalf("expected vars content to stay unchanged, got %q", got)
	}
}
