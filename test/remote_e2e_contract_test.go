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
		"MODE=\"${MODE:-offline-multinode}\"",
		"[--mode offline-multinode]",
		"offline-multinode)",
		"REMOTE_CMD=\"DECK_PREPARE_FORCE_REDOWNLOAD=${DECK_PREPARE_FORCE_REDOWNLOAD:-0} DECK_VAGRANT_PROVIDER=libvirt test/vagrant/run-offline-multinode-agent.sh\"",
		"REMOTE_GLOB=\"test/artifacts/offline-multinode-*\"",
		"MODE must be one of: offline-multinode",
	)
}

func TestRemoteVMDocModeContracts(t *testing.T) {
	root := projectRoot(t)
	docPath := filepath.Join(root, "docs", "archive", "testing", "remote-vm-test.md")
	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read doc: %v", err)
	}
	content := string(raw)

	expectContainsAll(t, content,
		"offline-multinode",
		"test/remote-e2e.sh",
		"--skip-cleanup",
	)
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
