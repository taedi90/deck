#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNNER="${ROOT_DIR}/test/e2e/vagrant/run-scenario.sh"

if [[ ! -x "${RUNNER}" ]]; then
  echo "[deck] missing required path: ${RUNNER}"
  exit 1
fi

DECK_VAGRANT_ENTRYPOINT="test/vagrant/run-offline-multinode-agent.sh" exec "${RUNNER}" "$@"
