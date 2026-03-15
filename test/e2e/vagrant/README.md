# Vagrant canonical E2E runner

This directory contains the canonical local Vagrant regression harness.

- `run-scenario.sh`: host-side entrypoint for scenario runs.
- `common.sh`: shared host-side helpers and step implementation.
- `run-scenario-vm.sh`: guest-side dispatcher used by the host runner.
- `render-workflows.sh`: copies the canonical workflow tree into the prepared bundle workspace.

The maintained path is `test/e2e/vagrant/run-scenario.sh` with workflows under `test/workflows/*`.
