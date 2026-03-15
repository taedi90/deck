<table width="100%"><tr><td valign="middle">

# deck

[English README](./README.md) | [문서 홈](./docs/README.md)

`deck`은 air-gapped 환경과 운영 제약이 큰 현장을 위한 워크플로 도구입니다. YAML 워크플로를 작성하고, 검증하고, 필요한 것을 번들로 묶어 반입하고, 현장에서 대상 장비에 로컬로 실행합니다.

</td><td align="right" valign="middle">
<img src="assets/logo.png" alt="deck logo" width="150" />
</td></tr></table>

## deck이 존재하는 이유

단절된 현장을 위한 운영 절차들 — Kubernetes 부트스트랩, 패키지 설치, 호스트 구성 — 은 보통 셸 스크립트로 시작해서 리뷰하기 어려울 만큼 커지곤 합니다. `deck`은 이런 절차에 더 명확한 구조를 부여합니다. 의도가 보이는 typed step, 반입 전 lint 검증, 그리고 워크플로와 함께 이동하는 self-contained 번들이 핵심입니다.

SSH 오케스트레이션 없이, 인터넷 연결 없이, 로컬 운영자가 직접 작업해야 하는 환경을 위해 만들었습니다. 라이브 API와 중앙 컨트롤러가 전제인 클라우드 배포 환경이라면 deck은 적합하지 않을 수 있습니다.

## 기본 흐름

1. 유지보수 세션에 맞는 YAML 워크플로를 작성하거나 조정합니다.
2. 반입 전과 실행 전에 워크플로 구조를 검증합니다.
3. 워크플로, 아티팩트, `deck` 바이너리를 함께 묶은 self-contained 번들을 만듭니다.
4. 승인된 경로를 통해 현장으로 번들을 반입합니다.
5. 변경이 필요한 장비에서 `deck`을 로컬로 실행합니다.

## 최소 워크플로

일반적인 호스트 변경에는 typed step을 우선 사용하고, 적합한 step kind가 없을 때만 `Command`를 사용하세요.

```yaml
role: apply
version: v1alpha1
steps:
  - id: write-repo-config
    apiVersion: deck/v1alpha1
    kind: File
    spec:
      action: write
      path: /etc/example.repo
      content: |
        [offline-base]
        name=offline-base
        baseurl=file:///srv/offline-repo
        enabled=1
        gpgcheck=0
```

## 설치

요구 사항:

- Go 1.23+
- Linux 타깃 환경

```bash
# 소스에서 바로 실행
go run ./cmd/deck --help

# 셸 자동완성 생성
go run ./cmd/deck completion bash

# 바이너리 설치
go install ./cmd/deck

# 확인
deck --help
```

## 빠른 시작

```bash
deck init --out ./demo
deck lint
deck lint --file ./demo/workflows/scenarios/apply.yaml

cd ./demo
deck prepare
deck bundle build --out ./bundle.tar
deck apply
```

단계별 가이드는 `docs/tutorials/quick-start.md`를 참고하세요.

## 셸 자동완성

```bash
deck completion bash
deck completion zsh
deck completion fish
deck completion powershell
```

자동완성 출력은 stdout으로 나갑니다. help는 `--help`로 요청할 때만 표시되고, 명령/플래그 오류는 usage 출력 없이 stderr로만 씁니다.

## 더 알아보기

- 문서 홈: `docs/README.md`
- 왜 deck인가: `docs/concepts/why-deck.md`
- 워크플로 모델: `docs/reference/workflow-model.md`
- CLI 레퍼런스: `docs/reference/cli.md`
- 예제 워크플로: `docs/examples/README.md`

## 기여 및 검증

변경을 보내기 전에 작업에 맞는 검증을 실행합니다.

```bash
go build -o ./deck ./cmd/deck
./deck --help
./deck completion bash
./deck completion zsh
./deck completion fish
./deck completion powershell
go test ./...
./deck lint
./deck lint --file docs/examples/vagrant-smoke-install.yaml
./deck lint --file test/workflows/scenarios/control-plane-bootstrap.yaml
./deck lint --file test/workflows/scenarios/worker-join.yaml
./deck lint --file test/workflows/scenarios/node-reset.yaml

# libvirt 기반 Vagrant가 있는 Linux 호스트
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-control-plane-bootstrap
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-node-reset
```

Kubernetes 회귀 테스트 레이아웃은 `test/workflows/` 아래에 있으며, 시나리오 진입점은 `test/workflows/scenarios/`, 재사용 가능한 조각은 `test/workflows/components/`, 공유 기본값은 `test/workflows/vars.yaml`에 있습니다.
로컬 Vagrant 실행은 `test/vagrant/.vagrant/`에 장비 상태를 유지하고, `test/artifacts/cache/bundles/<scenario>/`에 시나리오 캐시를, `test/artifacts/runs/<scenario>/<run-id>/`에 실행 아티팩트를 재사용합니다. 다른 경로를 원하면 `--art-dir`을 지정하거나 `--fresh`로 실행하세요.

## 라이선스

Apache-2.0. 자세한 내용은 `LICENSE`를 참고하세요.
