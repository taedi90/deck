//go:build !ai

package main

import "testing"

func TestAskStubReturnsClearError(t *testing.T) {
	_, err := runWithCapturedStdout([]string{"ask", "draft something"})
	if err == nil {
		t.Fatalf("expected ask stub error")
	}
	if got := err.Error(); got != "ask is not available in this build; rebuild with -tags ai or use the AI-ready binary" {
		t.Fatalf("unexpected error: %v", err)
	}
}
