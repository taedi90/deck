# deck

[English README](./README.md) | [문서 홈](./docs/README.md)

`deck`은 no-proxy, no-SSH, no-PXE, no-BMC, 그리고 인터넷 연결 자체를 전제할 수 없는 최악의 air-gapped 환경을 위한 단일 바이너리 인프라 워크플로 도구입니다.

온라인 중심 도구가 더 이상 편리하지 않고 오히려 짐이 되는 환경에서도 인프라 변경을 실용적으로 수행할 수 있게 만드는 것이 목적입니다.

## Visuals

![deck terminal demo](docs/assets/deck-cli.gif)

## Principles

- **Extreme Air-gap Focus**: `deck`은 완전히 격리된 환경에 최적화됩니다. 온라인 전제 기능이나 원격 오케스트레이션이 필요하다면 그 영역은 Ansible이나 Terraform 같은 도구에 맡기는 편이 적합합니다.
- **Hermetic & Self-contained**: `bundle.tar`는 오프라인 사이트에서 필요한 실행 로직, 데이터, 바이너리를 모두 포함하는 자급자족형 패키지여야 합니다.
- **Simple is the best**: 복잡도는 숨기지 않고 제거합니다. 핵심 사용자 흐름은 `pack -> apply`로 이해되어야 하며, 보조 명령은 존재 이유를 증명해야 합니다.
- **K8s friendly**: 워크플로는 YAML 기반이며, Kubernetes나 Helm, manifest 중심 도구에 익숙한 운영자도 빠르게 적응할 수 있도록 구성됩니다.
- 인터넷에 닿지 않는 사이트에서도 로컬 워크플로와 로컬 번들만으로 인프라를 변경할 수 있습니다.
- 운영 흐름을 작고 명확하게 유지합니다. 아티팩트를 준비하고, 반입하고, 현장에서 로컬로 적용합니다.

## Install

요구 사항:

- Go 1.22+
- Linux 타깃 환경

```bash
# 소스에서 바로 실행
go run ./cmd/deck --help

# 바이너리 설치
go install ./cmd/deck

# 확인
deck --help
```

## Quick Start

1. 시작용 작업 공간을 만듭니다.

```bash
deck init --out ./demo
```

2. `./demo/workflows/pack.yaml`, `./demo/workflows/apply.yaml`, `./demo/workflows/vars.yaml`을 환경에 맞게 수정합니다.

3. 패키징이나 적용 전에 워크플로를 검증합니다.

```bash
deck validate --file ./demo/workflows/apply.yaml
```

4. 자체 포함형 오프라인 번들을 만듭니다.

```bash
deck pack --out ./bundle.tar
```

5. 번들을 오프라인 사이트로 반입하고 현지에서 로컬 실행합니다.

```bash
deck apply
```

6. 필요하면 준비된 번들을 로컬 repo server 형태로 HTTP로 노출할 수 있습니다.

```bash
deck serve --root ./bundle --addr :8080
deck source set --server http://127.0.0.1:8080
deck list
deck health
```

단계별 가이드는 `docs/tutorials/quick-start.md`부터 시작하시면 됩니다.

## How deck works

1. YAML 워크플로를 작성하거나 생성합니다.
2. `pack`이 패키지, 이미지, 파일, 워크플로, `deck` 바이너리를 번들에 모읍니다.
3. 승인된 오프라인 반입 경로로 번들을 전달합니다.
4. `apply`가 SSH나 원격 제어 없이 현장에서 로컬로 실행됩니다.

## Workflow Model

워크플로 DSL은 YAML 기반이며 step 실행을 중심으로 구성됩니다. 워크플로는 `role`(`pack` 또는 `apply`)을 선언하고, top-level `steps` 또는 이름 있는 `phases`를 사용합니다.

```yaml
role: apply
version: v1alpha1
steps:
  - id: disable-swap
    apiVersion: deck/v1alpha1
    kind: RunCommand
    spec:
      command: ["swapoff", "-a"]
```

공통 step 기능:

- `when` 조건 실행
- `retry`, `timeout` 제어
- step 출력값을 다음 step에 전달하는 `register`
- 워크플로 및 각 step kind에 대한 JSON Schema 검증

## Bundle Contract

설계상 준비된 번들에는 다음이 포함될 수 있습니다.

- 오프라인 실행 입력을 담는 `workflows/`
- 수집된 아티팩트를 담는 `packages/`, `images/`, `files/`
- 자체 실행을 위한 `deck` 및 `files/deck`
- 아티팩트 체크섬 메타데이터를 담는 `.deck/manifest.json`

## Command Surface

- 핵심 흐름: `init`, `validate`, `pack`, `apply`
- 오프라인 repo 흐름: `serve`, `source`, `list`, `health`
- 번들 수명주기: `bundle`
- 계획 및 진단: `diff`, `doctor`, `logs`, `cache`, `service`

## Documentation Map

- 문서 홈: `docs/README.md`
- Quick start 튜토리얼: `docs/tutorials/quick-start.md`
- Offline Kubernetes 튜토리얼: `docs/tutorials/offline-kubernetes.md`
- CLI 레퍼런스: `docs/reference/cli.md`
- 워크플로 모델: `docs/reference/workflow-model.md`
- 번들 구조: `docs/reference/bundle-layout.md`
- 스키마 레퍼런스: `docs/reference/schema-reference.md`
- 서버 감사 로그 레퍼런스: `docs/reference/server-audit-log.md`
- 예제 워크플로: `docs/examples/README.md`
- 원본 JSON Schema: `docs/schemas/README.md`

## Scope and Non-goals

- `deck`은 air-gapped 준비, 패키징, 오프라인 설치, 결정적 로컬 실행에 집중합니다.
- 원격 오케스트레이션 프레임워크를 목표로 하지 않습니다.
- 항상 연결된 클라우드 중심 환경을 최적화 대상으로 삼지 않습니다.

## Contributing and Validation

변경 전에는 작업에 맞는 검증을 실행합니다.

```bash
go test ./...
go run ./cmd/deck validate --file <workflow.yaml>
```

## License

Apache-2.0 라이선스를 따릅니다. 자세한 내용은 `LICENSE`를 참고하세요.
