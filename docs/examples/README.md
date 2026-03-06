# docs/examples Test Cases

`docs/examples`는 문서 예제로 사용하며, 기본 검증은 `deck validate` 중심으로 수행한다.

## 케이스 목록

- 케이스 인덱스: `docs/examples/cases.tsv`
- 컬럼
  - `file`: 예제 YAML 파일명
  - `mode`: `validate` 또는 `run-install`

## 실행 방법

```bash
./deck validate --file docs/examples/<file>.yaml
```

`run-install` 케이스는 실제 VM 실행이 필요하므로 별도 수동 시나리오로 검증한다.
