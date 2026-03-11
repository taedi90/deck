# deck

[English README](./README.md) | [문서 홈](./docs/README.md)

`deck`은 air-gapped 환경과 운영 제약이 큰 현장을 위한 단순한 워크플로 도구입니다.

더 큰 자동화 플랫폼이 맞지 않고, 커져버린 Bash 절차가 읽기 어렵고 검토하기 힘들어지는 상황을 위해 만들었습니다. `deck`의 모델은 작고 분명합니다. 워크플로를 작성하고, 검증하고, 필요한 것을 번들로 묶어 반입하고, 현장에서 로컬로 실행합니다.

## Visuals

![deck terminal demo](docs/assets/deck-cli.gif)

## Why deck exists

- **Air-gapped by design**: no SSH, no PXE, no BMC, 인터넷 비의존 환경을 기본으로 둡니다.
- **작은 도구, 제한된 문제**: 새로운 범용 자동화 플랫폼을 만들려는 것이 아닙니다.
- **Bash 확장 문제 완화**: 큰 운영 절차를 긴 shell 파일 대신 step과 phase로 읽을 수 있게 합니다.
- **개발자 친화적 형태**: CI/CD YAML, Kubernetes manifest에 익숙한 사람이 빠르게 적응할 수 있습니다.
- **번들 중심 실행**: 유지보수 전에 워크플로, 아티팩트, `deck` 바이너리를 함께 묶습니다.

## What deck is for

- 대상 호스트나 노드에서 로컬로 직접 실행해야 하는 반복 가능한 유지보수 절차
- 패키지, 이미지, 파일, 워크플로 입력을 오프라인으로 준비해 반입해야 하는 작업
- 더 큰 controller, agent, runtime stack 없이도 유지할 수 있는 단순한 도구가 필요한 팀
- Bash로 시작했지만 이제는 절차가 너무 커져 가독성과 리뷰 품질이 무너진 경우

## What deck is not for

- 항상 연결된 인프라를 원격 오케스트레이션해야 하는 경우
- Terraform, Pulumi, Ansible, Chef, Puppet 같은 범용 플랫폼의 대체를 기대하는 경우
- 장기 실행 control plane, fleet agent, policy engine이 필요한 경우
- live API와 중앙 controller가 정상 전제인 cloud-first 워크플로

## Why not just use Bash

- 작은 스크립트는 빠르지만, 큰 운영 절차는 그렇지 않습니다.
- Bash는 운영자의 의도보다 구현 디테일을 먼저 드러냅니다.
- 절차가 길어질수록 리뷰 품질이 빠르게 떨어집니다.
- 재사용, 검증, step 단위 reasoning이 약해집니다.

`deck`은 shell을 완전히 없애려는 도구가 아닙니다. 절차를 더 잘 보이게 구조화하고, `RunCommand`는 기본 작성 방식이 아니라 escape hatch로 남겨둡니다.

## Core flow

1. 유지보수 세션에 맞는 YAML 워크플로를 작성하거나 조정합니다.
2. 반입 전과 실행 전에 워크플로 구조를 검증합니다.
3. 워크플로, 아티팩트, `deck` 바이너리를 함께 묶은 self-contained bundle을 만듭니다.
4. 승인된 경로를 통해 현장으로 번들을 반입합니다.
5. 변경이 필요한 장비에서 `deck`을 로컬로 실행합니다.

## Minimal workflow

일반적인 호스트 변경에는 typed step을 우선 사용하고, 적절한 step kind가 없을 때만 `RunCommand`를 사용하세요.

```yaml
role: apply
version: v1alpha1
steps:
  - id: write-repo-config
    apiVersion: deck/v1alpha1
    kind: WriteFile
    spec:
      path: /etc/example.repo
      content: |
        [offline-base]
        name=offline-base
        baseurl=file:///srv/offline-repo
        enabled=1
        gpgcheck=0
```

## Install

Requirements:

- Go 1.22+
- Linux 타깃 환경

```bash
# 소스에서 바로 실행
go run ./cmd/deck --help

# 바이너리 설치
go install ./cmd/deck

# verify
deck --help
```

## Quick Start

```bash
deck init --out ./demo
deck validate --file ./demo/workflows/apply.yaml
deck validate --file ./demo/workflows/pack.yaml

cd ./demo
deck pack --out ./bundle.tar
deck apply
```

단계별 가이드는 `docs/tutorials/quick-start.md`부터 시작하시면 됩니다.

## Learn more

- Docs home: `docs/README.md`
- 왜 deck인가: `docs/concepts/why-deck.md`
- 워크플로 모델: `docs/reference/workflow-model.md`
- CLI 레퍼런스: `docs/reference/cli.md`
- 예제 워크플로: `docs/examples/README.md`

## Scope and non-goals

- `deck`은 disconnected 환경에서의 단순하고 로컬한 bundle-first 실행에 집중합니다.
- site-assisted 사용은 명시적이고 부가적인 선택지입니다. 로컬 운영자 경로를 대체하지 않습니다.
- `deck`은 원격 오케스트레이션 프레임워크가 아닙니다.
- `deck`은 광범위한 온라인 인프라 자동화를 최적화 대상으로 삼지 않습니다.

## Contributing and validation

변경을 보내기 전에 작업에 맞는 검증을 실행합니다.

```bash
go test ./...
go run ./cmd/deck validate --file <workflow.yaml>

# linux host with libvirt-backed vagrant
bash test/vagrant/run-offline-multinode-agent.sh
```

## License

Apache-2.0 라이선스를 따릅니다. 자세한 내용은 `LICENSE`를 참고하세요.
