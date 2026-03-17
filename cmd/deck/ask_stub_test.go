//go:build !ai

package main

import (
	"strings"
	"testing"
)

func TestAskCommandIsHiddenWithoutAITag(t *testing.T) {
	out, err := runWithCapturedStdout([]string{"--help"})
	if err != nil {
		t.Fatalf("expected help success, got %v", err)
	}
	if strings.Contains(out, "ask") {
		t.Fatalf("expected ask command to be hidden, got %q", out)
	}

	_, err = runWithCapturedStdout([]string{"ask", "draft something"})
	if err == nil {
		t.Fatalf("expected unknown command error")
	}
	if got := err.Error(); !strings.Contains(got, "unknown command \"ask\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}
