# 시나리오 템플릿 YAML

이 디렉터리는 legacy Vagrant shim 호환성 표식만 둔다. 실제 유지보수 기준 경로는 이곳이 아니라 `test/workflows/`다.

## 구조

- `test/workflows/components/`
  - bootstrap, join, node-reset이 함께 쓰는 공통 fragment
- `test/workflows/scenarios/`
  - prepare, control-plane bootstrap, worker join, node reset 진입 workflow
- `test/workflows/vars.yaml`
  - 공통 기본값
- `test/e2e/scenario-meta/`, `test/e2e/scenario-hooks/`
  - Vagrant harness 전용 메타데이터와 guest helper
- `offline-multinode/`
  - shim 호환성 표식만 유지하는 최소 디렉터리 (legacy workflow 본문은 유지하지 않음)

## 사용 원칙

- 새 시나리오 수정은 `test/workflows/`에서 한다.
- 이 디렉터리의 `offline-multinode` 트리는 migration window 동안 entrypoint 호환성 표식만 유지한다.
- `join` 계열 시나리오는 control-plane에서 생성된 `join.txt`를 참조한다.
