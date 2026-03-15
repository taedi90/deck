package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requireContainsAll(t *testing.T, content string, items ...string) {
	t.Helper()
	for _, item := range items {
		if !strings.Contains(content, item) {
			t.Fatalf("expected content to include %q", item)
		}
	}
}

func requireScriptHelpContainsAll(t *testing.T, scriptPath string, items ...string) {
	t.Helper()
	out, err := exec.Command("bash", scriptPath, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("run %s --help: %v\n%s", scriptPath, err, string(out))
	}
	help := string(out)
	requireContainsAll(t, help, items...)
}

func TestVagrantHarnessBehaviorCanonicalScripts(t *testing.T) {
	root := testProjectRoot(t)
	runnerPath := filepath.Join(root, "test", "e2e", "vagrant", "run-scenario.sh")
	vmdPath := filepath.Join(root, "test", "e2e", "vagrant", "run-scenario-vm.sh")
	renderPath := filepath.Join(root, "test", "e2e", "vagrant", "render-workflows.sh")
	if _, err := os.Stat(runnerPath); err != nil {
		t.Fatalf("stat canonical runner: %v", err)
	}
	if _, err := os.Stat(vmdPath); err != nil {
		t.Fatalf("stat canonical vm dispatcher: %v", err)
	}
	if _, err := os.Stat(renderPath); err != nil {
		t.Fatalf("stat workflow renderer: %v", err)
	}
	requireScriptHelpContainsAll(t, runnerPath, "--scenario", "--fresh-cache", "--art-dir")
	requireScriptHelpContainsAll(t, vmdPath, "prepare-bundle", "apply-scenario", "verify-scenario", "bootstrap|cluster|all")
}

func TestVagrantHarnessBehaviorFresh(t *testing.T) {
	root := testProjectRoot(t)
	artDir := filepath.Join("test", "tmp", "fresh-behavior")
	if err := os.MkdirAll(filepath.Join(root, artDir, "checkpoints"), 0o755); err != nil {
		t.Fatalf("mkdir art dir: %v", err)
	}
	bundleCacheDir := filepath.Join(root, "test", "artifacts", "cache", "bundles", "k8s-worker-join")
	stagingCacheDir := filepath.Join(root, "test", "artifacts", "cache", "staging", "k8s-worker-join")
	vagrantCacheDir := filepath.Join(root, "test", "artifacts", "cache", "vagrant", "k8s-worker-join")
	for _, dir := range []string{bundleCacheDir, stagingCacheDir, vagrantCacheDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir cache dir %s: %v", dir, err)
		}
		marker := filepath.Join(dir, "keep-sentinel")
		if err := os.WriteFile(marker, []byte("cache"), 0o644); err != nil {
			t.Fatalf("write cache sentinel %s: %v", marker, err)
		}
	}

	cmd := exec.Command("bash", "-lc", "ROOT_DIR='"+root+"'; DECK_VAGRANT_SCENARIO=k8s-worker-join; DECK_VAGRANT_RUN_ID=test-run; DECK_VAGRANT_ART_DIR='"+artDir+"'; source test/e2e/vagrant/common.sh; FRESH=1; FRESH_CACHE=0; refresh_layout_contracts; mkdir -p \"${ART_DIR_ABS}/logs\"; touch \"${ART_DIR_ABS}/run-sentinel\" \"${ART_DIR_ABS}/logs/log-sentinel\"; prepare_local_run_state; test ! -e \"${ART_DIR_ABS}/run-sentinel\"; test ! -e \"${ART_DIR_ABS}/logs/log-sentinel\"; test -e '"+filepath.Join(bundleCacheDir, "keep-sentinel")+"'; test -e '"+filepath.Join(stagingCacheDir, "keep-sentinel")+"'; test -e '"+filepath.Join(vagrantCacheDir, "keep-sentinel")+"'")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fresh behavior check failed: %v\n%s", err, string(out))
	}
}

func TestVagrantHarnessBehaviorFreshCache(t *testing.T) {
	root := testProjectRoot(t)
	artDir := filepath.Join("test", "tmp", "fresh-cache-behavior")
	if err := os.MkdirAll(filepath.Join(root, artDir), 0o755); err != nil {
		t.Fatalf("mkdir art dir: %v", err)
	}
	bundleDir := filepath.Join(root, "test", "artifacts", "cache", "bundles", "k8s-worker-join")
	stagingDir := filepath.Join(root, "test", "artifacts", "cache", "staging", "k8s-worker-join")
	vagrantDir := filepath.Join(root, "test", "artifacts", "cache", "vagrant", "k8s-worker-join")
	for _, dir := range []string{bundleDir, stagingDir, vagrantDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir cache dir %s: %v", dir, err)
		}
		touch := filepath.Join(dir, "sentinel")
		if err := os.WriteFile(touch, []byte("x"), 0o644); err != nil {
			t.Fatalf("write sentinel %s: %v", touch, err)
		}
	}
	cmd := exec.Command("bash", "-lc", "ROOT_DIR='"+root+"'; DECK_VAGRANT_SCENARIO=k8s-worker-join; DECK_VAGRANT_RUN_ID=test-run; DECK_VAGRANT_ART_DIR='"+artDir+"'; source test/e2e/vagrant/common.sh; FRESH=1; FRESH_CACHE=1; refresh_layout_contracts; prepare_local_run_state; test ! -e '"+filepath.Join(bundleDir, "sentinel")+"' && test ! -e '"+filepath.Join(stagingDir, "sentinel")+"' && test ! -e '"+filepath.Join(vagrantDir, "sentinel")+"'")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fresh-cache behavior check failed: %v\n%s", err, string(out))
	}
}

func TestVagrantHarnessBehaviorCacheReuse(t *testing.T) {
	root := testProjectRoot(t)
	artDir := filepath.Join("test", "tmp", "cache-reuse-behavior")
	checkpointDir := filepath.Join(root, artDir, "checkpoints")
	bundleDir := filepath.Join(root, "test", "artifacts", "cache", "bundles", "k8s-worker-join")
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
		t.Fatalf("mkdir checkpoints: %v", err)
	}
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("mkdir bundle dir: %v", err)
	}
	for _, marker := range []string{"cleanup.done", "collect.done", "verify-scenario.done"} {
		if err := os.WriteFile(filepath.Join(checkpointDir, marker), []byte("x"), 0o644); err != nil {
			t.Fatalf("write checkpoint marker %s: %v", marker, err)
		}
	}
	bundleSentinel := filepath.Join(bundleDir, "reuse-sentinel")
	if err := os.WriteFile(bundleSentinel, []byte("cache"), 0o644); err != nil {
		t.Fatalf("write bundle sentinel: %v", err)
	}

	cmd := exec.Command("bash", "-lc", "ROOT_DIR='"+root+"'; DECK_VAGRANT_SCENARIO=k8s-worker-join; DECK_VAGRANT_RUN_ID=test-run; DECK_VAGRANT_ART_DIR='"+artDir+"'; source test/e2e/vagrant/common.sh; FRESH=0; FRESH_CACHE=0; RESUME=1; STEP=''; FROM_STEP=''; TO_STEP=''; refresh_layout_contracts; CHECKPOINT_DIR='"+checkpointDir+"'; prepare_local_run_state; test \"${FROM_STEP}\" = prepare-bundle; test ! -e '"+filepath.Join(checkpointDir, "cleanup.done")+"'; test ! -e '"+filepath.Join(checkpointDir, "collect.done")+"'; test ! -e '"+filepath.Join(checkpointDir, "verify-scenario.done")+"'; test -e '"+bundleSentinel+"'")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cache-reuse behavior check failed: %v\n%s", err, string(out))
	}
}
