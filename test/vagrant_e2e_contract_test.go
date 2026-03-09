package test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalVagrantE2EScriptContracts(t *testing.T) {
	root := projectRoot(t)
	scriptPath := filepath.Join(root, "test", "vagrant", "run-offline-multinode-agent.sh")
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(raw)

	expectContainsAll(t, content,
		"Usage: test/vagrant/run-offline-multinode-agent.sh [options]",
		"ART_DIR_REL=\"${DECK_VAGRANT_ART_DIR:-test/artifacts/offline-multinode-local}\"",
		"PREPARED_BUNDLE_REL=\"test/artifacts/cache/offline-multinode-prepared-bundle\"",
		"PREPARED_BUNDLE_WORK_REL=\"test/artifacts/cache/offline-multinode-prepared-bundle-work\"",
		"RSYNC_STAGE_REL=\"test/artifacts/cache/offline-multinode-rsync-root\"",
		"DECK_VAGRANT_PROVIDER=\"libvirt\"",
		"DECK_VAGRANT_SYNC_TYPE=\"${DECK_VAGRANT_SYNC_TYPE:-rsync}\"",
		"DECK_VAGRANT_VM_PREFIX=\"${DECK_VAGRANT_VM_PREFIX:-deck-offline-multinode-local}\"",
		"DECK_VAGRANT_SKIP_CLEANUP=\"${DECK_VAGRANT_SKIP_CLEANUP:-1}\"",
		"RESUME=\"${DECK_VAGRANT_RESUME:-1}\"",
		"--step <name>",
		"--from-step <name>",
		"--to-step <name>",
		"--resume",
		"--fresh",
		"--art-dir <path>",
		"--skip-cleanup",
		"--cleanup",
		"--skip-collect",
		"assert-cluster",
		"enqueue-join",
		"wait-install",
		"artifacts already visible on host via shared workspace; skipping VM fetch",
		"collect fetch skipped",
		"9p shared folders are unavailable on this host; retrying with rsync",
		"prepare_rsync_stage_root()",
		"ensure_rsync_sync_source()",
		"required provider 'libvirt' is unavailable",
		"box/provider mismatch",
	)

	expectNotContainsAny(t, content,
		"DECK_VAGRANT_SYNC_TYPE=${DECK_VAGRANT_SYNC_TYPE}",
		"vagrant rsync control-plane worker worker-2",
		"start-agents",
		"wait-join",
		"start-agent",
		"verify-worker",
	)

	expectNotContainsAny(t, content,
		"DECK_REMOTE_",
		"--env-file",
		"remote-e2e.sh",
		"Run deck Vagrant E2E on remote host.",
	)
}

func TestLibvirtEnvScriptContracts(t *testing.T) {
	root := projectRoot(t)
	scriptPath := filepath.Join(root, "test", "vagrant", "libvirt-env.sh")
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	content := string(raw)

	expectContainsAll(t, content,
		"DECK_VAGRANT_HOME=\"${DECK_VAGRANT_HOME:-${DECK_BACKUP_ROOT}/vagrant/home}\"",
		"export VAGRANT_HOME=\"${DECK_VAGRANT_HOME}\"",
		"pool-info",
		"net-info default",
		"deck-vagrant",
	)

	expectNotContainsAny(t, content,
		"DECK_VAGRANT_DOTFILE_PATH",
		"VAGRANT_DOTFILE_PATH",
	)
}

func TestVagrantfileSyncDefaults(t *testing.T) {
	root := projectRoot(t)
	vagrantfilePath := filepath.Join(root, "test", "vagrant", "Vagrantfile")
	raw, err := os.ReadFile(vagrantfilePath)
	if err != nil {
		t.Fatalf("read Vagrantfile: %v", err)
	}
	content := string(raw)

	expectContainsAll(t, content,
		"sync_type = ENV.fetch(\"DECK_VAGRANT_SYNC_TYPE\", \"rsync\").strip",
		"sync_source = ENV.fetch(\"DECK_VAGRANT_SYNC_SOURCE\", \"../..\").strip",
		"sync_type = \"rsync\" if sync_type.empty?",
		"when \"9p\"",
		"type: \"9p\"",
		"nfs_version: 4",
		"nfs_udp: false",
		"type: \"nfs\"",
		"when \"rsync\"",
		"type: \"rsync\"",
		"rsync__exclude:",
		"DECK_VAGRANT_SYNC_SOURCE",
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

func expectNotContainsAny(t *testing.T, content string, unexpected ...string) {
	t.Helper()
	for _, item := range unexpected {
		if strings.Contains(content, item) {
			t.Fatalf("expected content to exclude %q", item)
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
