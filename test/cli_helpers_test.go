package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	deckBinaryOnce sync.Once
	deckBinaryPath string
	errDeckBinary  error
)

func buildDeckBinary(t *testing.T, repoRoot string) string {
	t.Helper()
	deckBinaryOnce.Do(func() {
		binDir, err := os.MkdirTemp("", "deck-test-bin-")
		if err != nil {
			errDeckBinary = err
			return
		}
		deckBinaryPath = filepath.Join(binDir, "deck")
		cmd := exec.Command("go", "build", "-o", deckBinaryPath, "./cmd/deck")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			errDeckBinary = execBuildError{err: err, output: string(out)}
		}
	})
	if errDeckBinary != nil {
		t.Fatalf("build deck test binary: %v", errDeckBinary)
	}
	return deckBinaryPath
}

type execBuildError struct {
	err    error
	output string
}

func (e execBuildError) Error() string {
	return e.err.Error() + "\n" + e.output
}

func runDeckCommand(t *testing.T, repoRoot string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(buildDeckBinary(t, repoRoot), args...)
	cmd.Dir = repoRoot
	return cmd.CombinedOutput()
}

func runBashScript(t *testing.T, root string, script string) []byte {
	t.Helper()
	cmd := exec.Command("bash", "-lc", script)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash contract check failed: %v\n%s", err, string(out))
	}
	return out
}
