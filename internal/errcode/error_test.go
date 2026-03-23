package errcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeThroughWrappedErrors(t *testing.T) {
	err := fmt.Errorf("outer: %w", Newf("E_TEST", "boom %d", 7))
	if !Is(err, "E_TEST") {
		t.Fatalf("expected code lookup to succeed: %v", err)
	}
	var coded *Error
	if !errors.As(err, &coded) {
		t.Fatalf("expected errors.As to find coded error: %v", err)
	}
	if coded.Code != "E_TEST" {
		t.Fatalf("unexpected code: %s", coded.Code)
	}
	if got := err.Error(); got != "outer: E_TEST: boom 7" {
		t.Fatalf("unexpected error text: %s", got)
	}
}
