# Vagrant Test Scripts

이 디렉터리는 Vagrant 기반 수동/원격 E2E 검증 스크립트와 VM 정의를 포함한다.

## 구성 파일

- `Vagrantfile`: control-plane + worker 2대(총 3노드) VM 정의
- `run-single-node-real.sh`: A. 단일 노드(real) 시나리오
- `run-smoke.sh`: B. 다중 노드(real) 시나리오
- `run-offline-multinode-agent.sh`: C. server/agent pull 시나리오
  - 기본 토폴로지: `control-plane` 1대 + `worker` 2대
  - 기본 박스: `generic/ubuntu2204`(control-plane), `bento/ubuntu-24.04`(worker), `generic/rocky9`(worker-2)
  - 단계 실행 옵션: `--step`, `--from-step`, `--to-step`, `--resume`, `--art-dir`
- `run-vm-ssh-preflight.sh`: VM 생성/SSH 접근 preflight 시나리오

## 시나리오 요약

- control-plane
  - 로컬 번들(`.ci/cache/prepare/*/bundle`) 마운트 경로 사용
  - `InstallPackages(source=local-repo)` + `KubeadmInit(mode=real)` 수행
- worker
  - 로컬 번들(`.ci/cache/prepare/*/bundle`) 마운트 경로 사용
  - `InstallPackages(source=local-repo)` + `KubeadmJoin(mode=real)` 수행

## 아티팩트

- `.ci/artifacts/smoke-*/`
- `.ci/artifacts/offline-multinode-agent-*/`
- `.ci/artifacts/vm-ssh-*/`

각 아티팩트 디렉터리에는 상태 파일, manifest, vagrant status가 저장된다.

각 시나리오는 verifier 스크립트로 필수 아티팩트 존재 여부를 검증한다.

## offline-multinode-agent 단계 실행

- Step 목록:
  - `prepare-host`, `up-vms`, `start-agents`, `start-server`
  - `enqueue-install`, `wait-install`, `enqueue-join`, `wait-join`
  - `assert-cluster`, `collect`, `cleanup`
- 체크포인트:
  - `.ci/artifacts/offline-multinode-agent-*/checkpoints/<step>.done`
- 재개 예시:
  - `test/vagrant/run-offline-multinode-agent.sh --from-step wait-install --to-step collect --resume --art-dir .ci/artifacts/offline-multinode-agent-<ts>`
