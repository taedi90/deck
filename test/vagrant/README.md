# Vagrant scenario runner

이 디렉터리는 Linux 호스트에서 libvirt 기반 Vagrant 회귀 테스트를 돌릴 때 쓰는 호스트 자산과 호환성 shim을 둔다. 현재 유지보수 기준 경로는 `test/workflows/*` 시나리오와 `test/e2e/vagrant/run-scenario.sh`다.

## 구성 파일

- `Vagrantfile`: control-plane + worker 2대(총 3노드) VM 정의
- `run-offline-multinode-agent.sh`: 이전 `offline-multinode` 호출 경로를 유지하는 임시 호환성 shim
- `run-offline-multinode-vm.sh`: guest 내부 legacy 호출을 유지하는 임시 호환성 shim
- `build-deck-binaries.sh`: 호스트에서 테스트용 `deck` 바이너리 빌드
- `libvirt-env.sh`: libvirt pool/network 및 Vagrant plugin/home 준비

## 실행 전제

- Linux 호스트
- `vagrant`, `virsh`, libvirt
- `vagrant-libvirt` 플러그인 사용 가능 상태

## 기본 실행

```bash
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-control-plane-bootstrap
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-node-reset
```

기본값은 반복 로컬 루프에 맞춰져 있다.

- shared folder 기본값: `rsync`
- 필요하면 `DECK_VAGRANT_SYNC_TYPE=9p` 또는 `DECK_VAGRANT_SYNC_TYPE=nfs`로 override할 수 있다.
- rsync 경로는 repo 전체가 아니라 `test/artifacts/cache/vagrant/<scenario>/rsync-root/`에 준비된 최소 실행 트리만 sync한다.
- NFS 경로는 `nfs_version: 4`, `nfs_udp: false`로 고정한다.
- 기본 artifact 경로: `test/artifacts/runs/<scenario>/<run-id>/`
- 기본 VM prefix: `deck-<scenario>-local`
- 기본 cleanup 동작: VM 유지
- 기본 실행은 완료된 이전 run이 있으면 canonical step 경계에 맞춰 `prepare-bundle`부터 다시 시작한다.

자주 쓰는 유지보수 옵션:

- bootstrap만 확인: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-control-plane-bootstrap`
- worker join 검증: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join`
- node reset 검증: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-node-reset`
- 특정 단계만 실행: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join --step up-vms`
- 중단 지점부터 재개: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join --resume --art-dir test/artifacts/runs/k8s-worker-join/local`
- 완전 새로 시작: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join --fresh`
- collect fetch 생략: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join --skip-collect`
- VM 정리까지 수행: `bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join --cleanup`

## 아티팩트 경로

- `test/artifacts/runs/<scenario>/<run-id>/`
- `test/artifacts/cache/bundles/<scenario>/...`
- `test/artifacts/cache/staging/<scenario>/...`
- `test/artifacts/cache/vagrant/<scenario>/...`
- `test/vagrant/.vagrant/`

주요 출력:

- `checkpoints/<step>.done`
- `error-<step>.log`
- `cluster-nodes.txt`
- `result.json`
- `pass.txt`
- 공유 prepared bundle cache는 run별 artifact 디렉터리가 아니라 `test/artifacts/cache/bundles/<scenario>/...`에 유지된다.
- host-side bundle 작업 경로는 `test/artifacts/cache/staging/<scenario>/...`를 사용한다.
- rsync 모드에서는 guest가 실제로 읽는 파일만 `test/artifacts/cache/vagrant/<scenario>/...`에 staging 한 뒤 `/workspace`로 sync한다.
- Vagrant machine state는 기본 `.vagrant` 경로인 `test/vagrant/.vagrant/`에 유지된다.
- `nfs`/`9p` shared folder에서 결과 파일이 호스트에 바로 보이면 collect는 fetch 대신 검증만 수행한다.
- 호환성 창 동안 legacy `offline-multinode-pass.txt`, `offline-multinode-result.txt`도 함께 기록되지만 기본 계약은 `pass.txt`, `result.json`이다.

## 단계 실행

이 문서는 Vagrant 회귀 테스트 유지보수용이다. 제품의 권장 사용자 흐름은 문서 본편의 로컬 세션 경로인 `diff -> doctor -> apply`다.

- 내부 회귀 흐름은 호스트 준비, VM 기동, 시나리오 실행, 검증 수집, 정리 순서로 이해하면 된다.
- 시나리오별 워크플로는 `test/workflows/k8s-control-plane-bootstrap`, `test/workflows/k8s-worker-join`, `test/workflows/k8s-node-reset`에서 관리한다.
- bootstrap, join, node-reset 공통 조각은 `test/workflows/_shared/k8s/`에서 관리한다.
- 반복 로컬 실행은 기본적으로 같은 artifact 경로와 같은 VM prefix를 재사용한다.
- 재실행이 필요하면 `--from-step`, `--to-step`, `--resume`, `--art-dir`로 범위를 좁힌다.
- `--art-dir`를 바꿔도 prepared bundle은 공유 cache 경로를 재사용한다.
- 상태를 완전히 초기화하려면 `rm -rf test/vagrant/.vagrant test/artifacts/runs/k8s-worker-join/local test/artifacts/cache/bundles/k8s-worker-join test/artifacts/cache/staging/k8s-worker-join test/artifacts/cache/vagrant/k8s-worker-join` 후 다시 실행한다.

## 호환성 메모

- `test/vagrant/run-offline-multinode-agent.sh`와 `test/vagrant/run-offline-multinode-vm.sh`는 한 번의 migration window 동안만 남겨둔 shim이다.
- 새 유지보수나 문서 갱신은 shim 경로가 아니라 `test/e2e/vagrant/run-scenario.sh`와 `test/workflows/*`를 기준으로 한다.
