#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:?root dir required}"
TARGET_DIR="${2:?target dir required}"
SCENARIO_ID="${3:-${DECK_VAGRANT_SCENARIO:-k8s-worker-join}}"

CANONICAL_ROOT="${ROOT_DIR}/test/workflows"
COMPAT_ROOT="${ROOT_DIR}/test/vagrant/workflows/offline-multinode"

mkdir -p "${TARGET_DIR}"

if [[ -d "${CANONICAL_ROOT}" ]]; then
  cp -a "${CANONICAL_ROOT}/." "${TARGET_DIR}/"
fi

if [[ "${SCENARIO_ID}" == "offline-multinode" ]] && [[ -d "${COMPAT_ROOT}" ]]; then
  mkdir -p "${TARGET_DIR}/offline-multinode"
  cp -a "${COMPAT_ROOT}/." "${TARGET_DIR}/offline-multinode/"
fi

cat > "${TARGET_DIR}/pack.yaml" <<EOF
role: pack
version: v1alpha1
imports:
  - scenarios/prepare.yaml
EOF
cat > "${TARGET_DIR}/apply.yaml" <<'EOF'
role: apply
version: v1alpha1
imports:
  - scenarios/__SCENARIO__.yaml
EOF
python3 - <<'PY' "${TARGET_DIR}/apply.yaml" "${SCENARIO_ID}"
from pathlib import Path
import sys

path = Path(sys.argv[1])
scenario = sys.argv[2]
path.write_text(path.read_text().replace("__SCENARIO__", scenario))
PY
