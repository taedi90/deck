package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2ELayoutContracts(t *testing.T) {
	root := testProjectRoot(t)
	runnerPath := filepath.Join(root, "test", "e2e", "vagrant", "run-scenario.sh")
	renderPath := filepath.Join(root, "test", "e2e", "vagrant", "render-workflows.sh")
	scenarioHelperPath := filepath.Join(root, "test", "e2e", "vagrant", "run-scenario-vm-scenario.sh")
	if _, err := os.Stat(runnerPath); err != nil {
		t.Fatalf("stat canonical runner: %v", err)
	}
	if _, err := os.Stat(renderPath); err != nil {
		t.Fatalf("stat workflow renderer: %v", err)
	}
	if _, err := os.Stat(scenarioHelperPath); err != nil {
		t.Fatalf("stat scenario helper: %v", err)
	}
	requireScriptHelpContainsAll(t, runnerPath, "--scenario", "--resume", "--fresh", "--fresh-cache", "--art-dir")

	layoutContractCmd := "ROOT_DIR='" + root + "'; DECK_VAGRANT_SCENARIO=k8s-worker-join; DECK_VAGRANT_RUN_ID=contract-run; DECK_VAGRANT_CACHE_KEY=contract-cache; source '" + filepath.Join(root, "test", "e2e", "vagrant", "common.sh") + "'; parse_args --art-dir test/tmp/e2e-layout-contract-run; test \"${ART_DIR_REL}\" = test/tmp/e2e-layout-contract-run; test \"${CHECKPOINT_DIR}\" = \"${ROOT_DIR}/test/tmp/e2e-layout-contract-run/checkpoints\"; test \"${RUN_LOG_DIR}\" = \"${ROOT_DIR}/test/tmp/e2e-layout-contract-run/logs\"; test \"${RUN_REPORT_DIR}\" = \"${ROOT_DIR}/test/tmp/e2e-layout-contract-run/reports\"; test \"${RUN_BUNDLE_SOURCE_FILE}\" = \"${ROOT_DIR}/test/tmp/e2e-layout-contract-run/bundle-source.txt\"; refresh_layout_contracts; test \"${PREPARED_BUNDLE_REL}\" = test/artifacts/cache/bundles/k8s-worker-join/contract-cache; test \"${PREPARED_BUNDLE_WORK_REL}\" = test/artifacts/cache/staging/k8s-worker-join/contract-cache; test \"${RSYNC_STAGE_REL}\" = test/artifacts/cache/vagrant/k8s-worker-join/rsync-root"
	runBashScript(t, root, layoutContractCmd)

	tmp := t.TempDir()
	bootstrapContractCmd := "ART_DIR='" + filepath.Join(tmp, "bootstrap") + "'; SERVER_URL=http://127.0.0.1:18080; E2E_SCENARIO=k8s-control-plane-bootstrap; E2E_RUN_ID=run-a; E2E_PROVIDER=libvirt; E2E_CACHE_KEY=cache-a; E2E_STARTED_AT=2026-01-01T00:00:00Z; VERIFY_STAGE_DEFAULT=bootstrap; USES_WORKERS=0; REQUIRES_RESET_PROOF=0; export SERVER_URL E2E_SCENARIO E2E_RUN_ID E2E_PROVIDER E2E_CACHE_KEY E2E_STARTED_AT VERIFY_STAGE_DEFAULT USES_WORKERS REQUIRES_RESET_PROOF; source '" + scenarioHelperPath + "'; mkdir -p \"${ART_DIR}\"; write_result_contract; test -f \"${ART_DIR}/pass.txt\"; python3 - <<'PY' \"${ART_DIR}/result.json\"\nimport json\nimport sys\npath = sys.argv[1]\nwith open(path, 'r', encoding='utf-8') as fp:\n    data = json.load(fp)\nassert data['scenario'] == 'k8s-control-plane-bootstrap'\nassert data['result'] == 'PASS'\nevidence = data['evidence']\nassert 'workerApply' not in evidence\nassert 'workerReset' not in evidence\nPY"
	runBashScript(t, root, bootstrapContractCmd)

	nodeResetContractCmd := "ART_DIR='" + filepath.Join(tmp, "node-reset") + "'; SERVER_URL=http://127.0.0.1:18080; E2E_SCENARIO=k8s-node-reset; E2E_RUN_ID=run-b; E2E_PROVIDER=libvirt; E2E_CACHE_KEY=cache-b; E2E_STARTED_AT=2026-01-01T00:00:00Z; VERIFY_STAGE_DEFAULT=all; USES_WORKERS=1; REQUIRES_RESET_PROOF=1; export SERVER_URL E2E_SCENARIO E2E_RUN_ID E2E_PROVIDER E2E_CACHE_KEY E2E_STARTED_AT VERIFY_STAGE_DEFAULT USES_WORKERS REQUIRES_RESET_PROOF; source '" + scenarioHelperPath + "'; mkdir -p \"${ART_DIR}\"; write_result_contract; test -f \"${ART_DIR}/pass.txt\"; python3 - <<'PY' \"${ART_DIR}/result.json\"\nimport json\nimport sys\npath = sys.argv[1]\nwith open(path, 'r', encoding='utf-8') as fp:\n    data = json.load(fp)\nassert data['scenario'] == 'k8s-node-reset'\nassert data['result'] == 'PASS'\nevidence = data['evidence']\nassert evidence['workerApply'] == 'worker-apply-done.txt'\nassert evidence['worker2Apply'] == 'worker-2-apply-done.txt'\nassert evidence['workerReset'] == 'worker-reset-done.txt'\nassert evidence['workerRejoin'] == 'worker-rejoin-done.txt'\nassert evidence['resetState'] == 'reports/reset-state.txt'\nPY"
	runBashScript(t, root, nodeResetContractCmd)

	upgradeContractCmd := "ART_DIR='" + filepath.Join(tmp, "upgrade") + "'; SERVER_URL=http://127.0.0.1:18080; E2E_SCENARIO=k8s-upgrade; E2E_RUN_ID=run-c; E2E_PROVIDER=libvirt; E2E_CACHE_KEY=cache-c; E2E_STARTED_AT=2026-01-01T00:00:00Z; VERIFY_STAGE_DEFAULT=upgrade; USES_WORKERS=0; REQUIRES_RESET_PROOF=0; export SERVER_URL E2E_SCENARIO E2E_RUN_ID E2E_PROVIDER E2E_CACHE_KEY E2E_STARTED_AT VERIFY_STAGE_DEFAULT USES_WORKERS REQUIRES_RESET_PROOF; source '" + scenarioHelperPath + "'; mkdir -p \"${ART_DIR}\"; write_result_contract; test -f \"${ART_DIR}/pass.txt\"; python3 - <<'PY' \"${ART_DIR}/result.json\"\nimport json\nimport sys\npath = sys.argv[1]\nwith open(path, 'r', encoding='utf-8') as fp:\n    data = json.load(fp)\nassert data['scenario'] == 'k8s-upgrade'\nassert data['result'] == 'PASS'\nevidence = data['evidence']\nassert evidence['upgradeVersion'] == 'reports/upgrade-version.txt'\nassert evidence['upgradeNodes'] == 'reports/upgrade-nodes.txt'\nassert 'workerApply' not in evidence\nPY"
	runBashScript(t, root, upgradeContractCmd)

	hostMetadataCmd := "ROOT_DIR='" + root + "'; DECK_VAGRANT_SCENARIO=k8s-control-plane-bootstrap; source '" + filepath.Join(root, "test", "e2e", "vagrant", "common.sh") + "'; load_scenario_metadata; test \"${SCENARIO_METADATA_LOADED}\" = 1; test \"${SCENARIO_METADATA_NODES}\" = control-plane; test \"${SCENARIO_METADATA_USES_WORKERS}\" = 0"
	runBashScript(t, root, hostMetadataCmd)

	upgradeMetadataCmd := "ROOT_DIR='" + root + "'; DECK_VAGRANT_SCENARIO=k8s-upgrade; source '" + filepath.Join(root, "test", "e2e", "vagrant", "common.sh") + "'; load_scenario_metadata; test \"${SCENARIO_METADATA_LOADED}\" = 1; test \"${SCENARIO_METADATA_KUBERNETES_VERSION}\" = v1.30.1; test \"${SCENARIO_METADATA_UPGRADE_KUBERNETES_VERSION}\" = v1.31.0"
	runBashScript(t, root, upgradeMetadataCmd)

	guestHelperCmd := "ROOT_DIR='" + root + "'; E2E_SCENARIO=k8s-control-plane-bootstrap; source '" + scenarioHelperPath + "'; source_scenario_vm_helper >/dev/null 2>&1; declare -F bootstrap_prepare >/dev/null"
	runBashScript(t, root, guestHelperCmd)

	upgradeHelperCmd := "ROOT_DIR='" + root + "'; E2E_SCENARIO=k8s-upgrade; source '" + scenarioHelperPath + "'; source_scenario_vm_helper >/dev/null 2>&1; declare -F upgrade_apply >/dev/null"
	runBashScript(t, root, upgradeHelperCmd)

	renderDir := filepath.Join(tmp, "rendered")
	cmd := exec.Command("bash", filepath.Join(root, "test", "e2e", "vagrant", "render-workflows.sh"), root, renderDir)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("render prepared bundle workflows contract check failed: %v\n%s", err, string(out))
	}
	applyContent, err := os.ReadFile(filepath.Join(renderDir, "scenarios", "control-plane-bootstrap.yaml"))
	if err != nil {
		t.Fatalf("read rendered scenario workflow: %v", err)
	}
	if !strings.Contains(string(applyContent), "bootstrap.yaml") {
		t.Fatalf("expected rendered scenario workflow to keep canonical imports, got:\n%s", string(applyContent))
	}
	upgradeContent, err := os.ReadFile(filepath.Join(renderDir, "scenarios", "upgrade.yaml"))
	if err != nil {
		t.Fatalf("read rendered upgrade workflow: %v", err)
	}
	if !strings.Contains(string(upgradeContent), "upgrade-control-plane") {
		t.Fatalf("expected rendered upgrade workflow to keep upgrade steps, got:\n%s", string(upgradeContent))
	}
}

func testProjectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, ".."))
}
