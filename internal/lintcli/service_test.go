package lintcli

import (
	"context"
	"errors"
	"testing"
)

func TestBuildReportCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := BuildReport(ctx, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
