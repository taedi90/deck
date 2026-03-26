# Ask component shape

Generated from workflow topology and component fragment guidance.

- Imports are only valid under `phases[].imports` and resolve from `workflows/components/`
- Component files are fragment documents, not full workflow documents
- Allowed root keys for component fragments: `steps`

## Import example

```yaml
phases:
  - name: preflight
    imports:
      - path: check-host.yaml
```

## Component example

```yaml
steps:
  - id: check-host
    kind: CheckHost
    spec:
      checks: [os, arch, swap]
      failFast: true
```
