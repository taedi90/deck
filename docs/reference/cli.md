# CLI Reference

This page describes the current `deck` command surface, starting with the default local operator flow.

## Default local flow

- `init`: create starter workflow files under `workflows/`
- `validate`: validate a workflow file against the top-level workflow schema and the relevant step schema
- `pack`: gather artifacts, copy workflows, embed the `deck` binary, and write `bundle.tar`
- `diff`: inspect which apply steps would run or skip before you execute
- `doctor`: generate a validation report for workflow inputs and referenced artifacts
- `apply`: execute the `apply` workflow locally

## Optional site-local helpers

- `serve`: expose a prepared bundle root over HTTP inside the air gap when you explicitly want a shared local source
- `list`: inspect available workflows from a local bundle root or an explicitly chosen server
- `health`: check `/healthz` on an explicitly chosen server
- `logs`: read server audit logs when you are using `deck serve`

These commands support site-assisted workflows. They do not replace the default local execution path.

## Other lifecycle commands

- `bundle`: bundle lifecycle operations
- `cache`: inspect or clean the artifact cache
- `node`: inspect or manage the stable local `node_id`
- `site`: manage local release/session/assignment lifecycle at the site store

## Common examples

```bash
deck init --out ./demo
deck validate --file ./demo/workflows/apply.yaml
deck pack --out ./bundle.tar
tar -xf ./bundle.tar
cd ./bundle
deck diff --file ./workflows/apply.yaml
deck doctor --file ./workflows/apply.yaml --out ./reports/doctor.json
deck apply --file ./workflows/apply.yaml
```

Optional site-local inspection example:

```bash
deck serve --root ./bundle --addr :8080
deck list --server http://127.0.0.1:8080
deck health --server http://127.0.0.1:8080
```

## Notes

- `pack` expects a workflow directory containing `pack.yaml`, `apply.yaml`, and `vars.yaml`.
- `apply` defaults to the `install` phase when phases are used.
- Prefer typed step kinds for common host changes. `RunCommand` remains available, but it is best kept for last-resort cases.
