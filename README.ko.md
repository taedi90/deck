# deck

<img align="right" src="assets/logo.png" width="120" alt="logo">

[English README](./README.md) | [문서 홈](./docs/README.md)

**폐쇄망 운영을 위한 구조화된 워크플로우 : prepare, bundle, apply를 단일 바이너리로!**

<br clear="right" />

## 프로젝트 소개

인터넷이 차단된 폐쇄망(Air-gapped) 환경에서 쿠버네티스 구축, 패키지 설치, 호스트 설정 등의 운영 작업은 대개 셸 스크립트로 시작됩니다. 하지만 운영 절차가 복잡해질수록 스크립트의 크기는 걷잡을 수 없이 커지고, 결국 유지보수나 검토가 불가능한 수준에 이르게 됩니다.

`deck`은 이러한 한계를 극복하기 위해 설계되었습니다. 불안정한 Bash 스크립트를 명확한 의도가 드러나는 '타입 기반 단계(Typed Steps)'로 대체하고, 실행 전 검증 과정을 거칩니다. 또한 작업에 필요한 모든 리소스를 독립적인 번들(Self-contained Bundle)로 묶어, 현장에서 추가 의존성 없이 즉시 실행할 수 있는 환경을 제공합니다.

## 시작하기

워크플로우 작성부터 번들 제작, 현장 실행까지의 과정은 매우 직관적입니다.

```bash
# 1. 데모 프로젝트 초기화
deck init --out ./demo

cd ./demo

# 2. 워크플로우 검증 (구조 및 문법 체크)
deck lint

# 3. 정의된 아티팩트(파일, 패키지 등) 수집 및 준비
deck prepare

# 4. 현장 반입용 단일 번들 빌드
deck bundle build --out ./bundle.tar

# 5. 대상 장비에서 워크플로우 실행
deck apply
```

상세한 사용법은 [빠른 시작 가이드](docs/guides/quick-start.md)에서 확인하실 수 있습니다.

## 핵심 기능

- **타입 기반 단계**: File, Package, ManageService — 목적이 명확한 step으로 셸 블록을 대체합니다. 작업 의도가 코드에서 바로 드러납니다.
- **사전 검증**: `lint`으로 현장 투입 전에 워크플로우 오류를 잡아냅니다.
- **자기 완결형 번들**: 워크플로우, 아티팩트, `deck` 바이너리를 단일 아카이브로 패키징합니다. 현장에서 의존성 문제가 없습니다.
- **폐쇄망 전용**: 실행 시 SSH, 컨트롤 플레인, 인터넷 연결 모두 불필요합니다.

## 설치

**요구 사항:**
- **Go 1.25 이상** (모든 OS에서 빌드 및 `prepare` 실행 가능)
- **Linux 실행 환경** (RHEL, Ubuntu 계열 지원; `apply` 단계에서 필요)

릴리즈 바이너리와 Linux 패키지는 GitHub Releases 페이지에 게시합니다. Homebrew는 `homebrew-core`가 아니라 Airgap Castaways tap으로 제공합니다.

```bash
# Homebrew tap
brew tap Airgap-Castaways/tap
brew install Airgap-Castaways/tap/deck
```

릴리즈 자산은 `https://github.com/Airgap-Castaways/deck/releases`에서 내려받을 수 있습니다.

```bash
# 소스에서 빌드 및 설치
go install ./cmd/deck

# 설치 확인
deck version
```

### 셸 자동완성 설정

현재 세션에서 자동완성을 활성화하려면 다음 명령어를 실행하세요.

```bash
source <(deck completion bash) # bash 사용 시
source <(deck completion zsh)  # zsh 사용 시
deck completion fish | source  # fish 사용 시
```

영구적으로 적용하려면, 사용하는 셸의 시작 파일(예: `~/.bashrc`, `~/.zshrc`)에 해당 명령어를 추가하세요. 지원되는 모든 셸에 대한 자세한 내용은 [CLI 레퍼런스](docs/reference/cli.md#shell-completion)를 참고하세요.

## 상세 문서

- [가이드](docs/guides/README.md)
- [핵심 개념](docs/core-concepts/README.md)
- [레퍼런스](docs/reference/README.md)
- [기여하기](docs/contributing/README.md)

## 라이선스

Apache-2.0. 자세한 내용은 `LICENSE` 파일을 참고하세요.
