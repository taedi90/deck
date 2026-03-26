# Ask vars guidance

Generated from vars guidance metadata.

- Path: `workflows/vars.yaml`
- Prefer vars for repeated package lists, repository URLs, versions, service names, paths, and ports
- Avoid vars for runtime-only outputs, one-off literals, and typed fields that must stay native arrays, objects, or constrained literals
- `workflows/vars.yaml` must remain plain YAML data only
