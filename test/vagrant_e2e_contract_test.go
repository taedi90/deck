package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func expectContainsAll(t *testing.T, content string, expected ...string) {
	t.Helper()
	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf("expected content to include %q", item)
		}
	}
}

func TestVagrantRunnerThinInterfaceContracts(t *testing.T) {
	root := projectRoot(t)
	runnerPath := filepath.Join(root, "test", "e2e", "vagrant", "run-scenario.sh")
	if _, err := os.Stat(runnerPath); err != nil {
		t.Fatalf("stat runner: %v", err)
	}
	out, err := exec.Command("bash", runnerPath, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("run runner --help: %v\n%s", err, string(out))
	}
	expectContainsAll(t, string(out), "--scenario", "--resume", "--art-dir")
}

func TestLibvirtEnvScriptContracts(t *testing.T) {
	root := projectRoot(t)
	scriptPath := filepath.Join(root, "test", "vagrant", "libvirt-env.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("stat libvirt env helper: %v", err)
	}

	cmd := exec.Command("bash", "-lc", "source '"+scriptPath+"'; declare -F prepare_libvirt_environment >/dev/null; test -n \"${DECK_LIBVIRT_POOL_NAME}\"; test -n \"${DECK_LIBVIRT_URI}\"; test -n \"${DECK_LIBVIRT_POOL_PATH}\"; test -n \"${DECK_VAGRANT_HOME}\"")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("libvirt env script contract check failed: %v\n%s", err, string(out))
	}
}

func TestVagrantfileSyncDefaults(t *testing.T) {
	root := projectRoot(t)
	vagrantfilePath := filepath.Join(root, "test", "vagrant", "Vagrantfile")
	if _, err := os.Stat(vagrantfilePath); err != nil {
		t.Fatalf("stat Vagrantfile: %v", err)
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, ".."))
}
