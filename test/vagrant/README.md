# Vagrant offline-multinode scenario

이 디렉터리는 Linux 호스트에서 libvirt 기반 Vagrant로 실행하는 `offline-multinode` 단일 회귀 시나리오만 유지한다.

## 구성 파일

- `Vagrantfile`: control-plane + worker 2대(총 3노드) VM 정의
- `run-offline-multinode-agent.sh`: 로컬 Vagrant/libvirt 회귀 테스트 엔트리포인트
  - 내부 단계 실행 옵션: `--step`, `--from-step`, `--to-step`, `--resume`, `--fresh`, `--art-dir`, `--skip-cleanup`, `--cleanup`, `--skip-collect`
- `run-offline-multinode-vm.sh`: VM 내부 단계 실행 스크립트
- `build-deck-binaries.sh`: 호스트에서 테스트용 `deck` 바이너리 빌드
- `libvirt-env.sh`: libvirt pool/network 및 Vagrant plugin/home 준비

## 실행 전제

- Linux 호스트
- `vagrant`, `virsh`, libvirt
- `vagrant-libvirt` 플러그인 사용 가능 상태

## 기본 실행

```bash
bash test/vagrant/run-offline-multinode-agent.sh
```

기본값은 반복 로컬 루프에 맞춰져 있다.

- shared folder 기본값: `rsync`
- 필요하면 `DECK_VAGRANT_SYNC_TYPE=9p` 또는 `DECK_VAGRANT_SYNC_TYPE=nfs`로 override할 수 있다.
- rsync 경로는 repo 전체가 아니라 `test/artifacts/cache/offline-multinode-rsync-root/`에 준비된 최소 실행 트리만 sync한다.
- NFS 경로는 `nfs_version: 4`, `nfs_udp: false`로 고정한다.
- 기본 artifact 경로: `test/artifacts/offline-multinode-local/`
- 기본 VM prefix: `deck-offline-multinode-local`
- 기본 cleanup 동작: VM 유지
- 기본 실행은 완료된 이전 run이 있으면 `start-server`부터 다시 시작한다.

자주 쓰는 유지보수 옵션:

- 특정 단계만 실행: `bash test/vagrant/run-offline-multinode-agent.sh --step up-vms`
- 중단 지점부터 재개: `bash test/vagrant/run-offline-multinode-agent.sh --resume --art-dir test/artifacts/offline-multinode-local`
- 완전 새로 시작: `bash test/vagrant/run-offline-multinode-agent.sh --fresh`
- collect fetch 생략: `bash test/vagrant/run-offline-multinode-agent.sh --skip-collect`
- VM 정리까지 수행: `bash test/vagrant/run-offline-multinode-agent.sh --cleanup`

## 아티팩트 경로

- `test/artifacts/offline-multinode-*/`
- `test/artifacts/cache/offline-multinode-prepared-bundle/`
- `test/artifacts/cache/offline-multinode-prepared-bundle-work/`
- `test/artifacts/cache/offline-multinode-rsync-root/`
- `test/vagrant/.vagrant/`

주요 출력:

- `checkpoints/<step>.done`
- `error-<step>.log`
- `cluster-nodes.txt`
- `offline-multinode-pass.txt`
- 공유 prepared bundle cache는 run별 artifact 디렉터리가 아니라 `test/artifacts/cache/offline-multinode-prepared-bundle/`에 유지된다.
- host-side bundle 작업 경로도 `test/artifacts/cache/offline-multinode-prepared-bundle-work/`로 고정된다.
- rsync 모드에서는 guest가 실제로 읽는 파일만 `test/artifacts/cache/offline-multinode-rsync-root/`에 staging 한 뒤 `/workspace`로 sync한다.
- Vagrant machine state는 기본 `.vagrant` 경로인 `test/vagrant/.vagrant/`에 유지된다.
- `nfs`/`9p` shared folder에서 결과 파일이 호스트에 바로 보이면 collect는 fetch 대신 검증만 수행한다.

## 단계 실행

이 문서는 Vagrant 회귀 테스트 유지보수용이다. 제품의 권장 사용자 흐름은 문서 본편의 로컬 세션 경로인 `diff -> doctor -> apply`다.

- 내부 회귀 흐름은 호스트 준비, VM 기동, 시나리오 실행, 검증 수집, 정리 순서로 이해하면 된다.
- 반복 로컬 실행은 기본적으로 같은 artifact 경로와 같은 VM prefix를 재사용한다.
- 재실행이 필요하면 `--from-step`, `--to-step`, `--resume`, `--art-dir`로 범위를 좁힌다.
- `--art-dir`를 바꿔도 prepared bundle은 공유 cache 경로를 재사용한다.
- 상태를 완전히 초기화하려면 `rm -rf test/vagrant/.vagrant test/artifacts/offline-multinode-local test/artifacts/cache/offline-multinode-prepared-bundle test/artifacts/cache/offline-multinode-prepared-bundle-work` 후 다시 실행한다.
