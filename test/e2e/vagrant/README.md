# Vagrant canonical E2E runner

This directory contains the canonical local Vagrant regression harness.

- `run-scenario.sh`: host-side entrypoint for scenario runs.
- `common.sh`: shared host-side helpers and step implementation.
- `run-scenario-vm.sh`: guest-side dispatcher used by the host runner.

The maintained path is `test/e2e/vagrant/run-scenario.sh` with workflows under `test/workflows/*`.
Legacy `test/vagrant/run-offline-multinode-*.sh` scripts are temporary compatibility shims that delegate to this canonical runner.
