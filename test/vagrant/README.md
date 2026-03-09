# Vagrant offline-multinode scenario

이 디렉터리는 Vagrant 기반 `offline-multinode` 단일 시나리오만 유지한다.

## 구성 파일

- `Vagrantfile`: control-plane + worker 2대(총 3노드) VM 정의
- 레거시 CI 하네스 스크립트 1종
  - 내부 단계 실행 옵션: `--step`, `--from-step`, `--to-step`, `--resume`, `--art-dir`
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

이 문서는 Vagrant 회귀 테스트 유지보수용이다. 권장 사용자 흐름은 문서 본편의 수동 세션 경로, `diff -> doctor -> apply`다.

- 내부 회귀 흐름은 호스트 준비, VM 기동, 시나리오 실행, 검증 수집, 정리 순서로만 이해하면 된다.
- 특정 중단 지점부터 다시 돌려야 하면 레거시 CI 하네스의 `--from-step`, `--to-step`, `--resume`, `--art-dir` 옵션을 쓴다.
