package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteE2EScriptModeContracts(t *testing.T) {
	root := projectRoot(t)
	scriptPath := filepath.Join(root, "test", "remote-e2e.sh")
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(raw)

	expectContainsAll(t, content,
		"[--mode single|smoke|vm-ssh|offline-multinode-agent]",
		"single)",
		"smoke)",
		"vm-ssh)",
		"REMOTE_CMD=\"DECK_PREPARE_FORCE_REDOWNLOAD=${DECK_PREPARE_FORCE_REDOWNLOAD:-0} DECK_VAGRANT_PROVIDER=libvirt test/vagrant/run-single-node-real.sh\"",
		"REMOTE_CMD=\"DECK_PREPARE_FORCE_REDOWNLOAD=${DECK_PREPARE_FORCE_REDOWNLOAD:-0} DECK_VAGRANT_PROVIDER=libvirt test/vagrant/run-smoke.sh\"",
		"REMOTE_CMD=\"DECK_VAGRANT_PROVIDER=libvirt test/vagrant/run-vm-ssh-preflight.sh\"",
		"REMOTE_GLOB=\".ci/artifacts/single-node-*\"",
		"REMOTE_GLOB=\".ci/artifacts/smoke-*\"",
		"REMOTE_GLOB=\".ci/artifacts/vm-ssh-*\"",
		"MODE must be one of: single, smoke, vm-ssh, offline-multinode-agent",
	)
}

func TestRemoteVMDocModeContracts(t *testing.T) {
	root := projectRoot(t)
	docPath := filepath.Join(root, "docs", "remote-vm-test.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	content := string(raw)

	expectContainsAll(t, content,
		"- `single`",
		"- `smoke`",
		"- `vm-ssh`",
		"--mode smoke",
	)
}

func TestVMSSHMatrixIncludesRequiredBoxes(t *testing.T) {
	root := projectRoot(t)
	required := []string{"generic/ubuntu2204", "bento/ubuntu-24.04", "generic/rocky9"}
	files := []string{filepath.Join(root, "test", "vagrant", "nightly-boxes-libvirt.txt")}

	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read nightly matrix file %s: %v", path, err)
		}
		content := string(raw)
		for _, box := range required {
			if !strings.Contains(content, box) {
				t.Fatalf("matrix file %s missing required box %s", path, box)
			}
		}
	}
}

func expectContainsAll(t *testing.T, content string, expected ...string) {
	t.Helper()
	for _, item := range expected {
		if !strings.Contains(content, item) {
			t.Fatalf("expected content to include %q", item)
		}
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
