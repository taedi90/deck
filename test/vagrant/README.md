# Vagrant offline-multinode scenario

이 디렉터리는 Vagrant 기반 `offline-multinode` 단일 시나리오만 유지한다.

## 구성 파일

- `Vagrantfile`: control-plane + worker 2대(총 3노드) VM 정의
- `run-offline-multinode-agent.sh`: 호스트 오케스트레이터
  - 단계 실행 옵션: `--step`, `--from-step`, `--to-step`, `--resume`, `--art-dir`
- `run-offline-multinode-vm.sh`: VM 내부 단계 실행 스크립트
- `build-deck-binaries.sh`: 호스트에서 테스트용 `deck` 바이너리 빌드
- `libvirt-env.sh`: libvirt pool/network 및 Vagrant 환경 준비

## 아티팩트 경로

- `test/artifacts/offline-multinode-*/`

주요 출력:

- `checkpoints/<step>.done`
- `error-<step>.log`
- `cluster-nodes.txt`
- `offline-multinode-pass.txt`

## 단계 실행

- Step 목록:
  - `prepare-host`, `up-vms`, `start-agents`, `start-server`
  - `enqueue-install`, `wait-install`, `enqueue-join`, `wait-join`
  - `assert-cluster`, `collect`, `cleanup`
- 재개 예시:
  - `test/vagrant/run-offline-multinode-agent.sh --from-step wait-install --to-step collect --resume --art-dir test/artifacts/offline-multinode-<ts>`
