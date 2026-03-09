# 시나리오 템플릿 YAML

이 디렉터리는 Vagrant 기반 `offline-multinode` 단일 시나리오 템플릿만 제공한다.

## 구조

- `offline-multinode/profile/`
  - `prepare.yaml`, `control-plane.yaml`, `worker.yaml`
- `offline-multinode/prepare/`
  - pack 단계 fragment
- `offline-multinode/apply/`
  - install 단계 fragment
  - 공통 fragment는 `offline-multinode/apply/common/`에 위치
- `offline-multinode/vars/`
  - profile에서 `varImports`로 가져오는 변수 파일

## 사용 원칙

- 템플릿은 로컬 Vagrant 회귀 테스트와 시나리오 유지보수의 기준점이며, 환경에 맞게 `vars`, `context`, 경로를 조정한다.
- `join` 계열 템플릿은 control-plane에서 생성된 `join.txt`를 참조한다.
