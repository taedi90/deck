# CLI Reference

The `deck` CLI is intentionally small.

It supports a simple operator flow: author the workflow, validate it, build the bundle, and run locally.

## Default local flow

- `init`: create starter workflow files under `workflows/`
- `completion`: generate shell completion for bash, zsh, fish, and PowerShell
- `validate`: validate a workflow file against the workflow and step schemas
- `prepare`: gather artifacts, copy workflows, embed the `deck` binary, and write `bundle.tar`
- `plan`: inspect which apply steps would run or skip before execution
- `doctor`: generate a report for preflight-style checks and diagnostics
- `apply`: execute the `apply` workflow locally

## Optional site-local helpers

- `server set`: save a default server URL and optional API token for commands that accept `--server` and `--api-token`
- `server up`: expose a prepared bundle root over HTTP inside the air gap when a shared local source is useful
- `server down`: stop a daemonized local server started with `deck server up -d`
- `server scenarios`: inspect available scenarios from a saved or explicitly chosen server
- `server health`: check `/healthz` on an explicit or saved server
- `server logs`: read local server audit logs from file or journal

These commands are additive. They do not replace the default local execution path.

## Shell completion

- `completion` is the only completion entrypoint, so normal command stdout stays reserved for command results.
- Supported shells: `bash`, `zsh`, `fish`, `powershell`

```bash
deck completion bash
deck completion zsh
deck completion fish
deck completion powershell
```

## Other lifecycle commands

- `bundle`: bundle lifecycle operations
- `cache`: inspect or clean the artifact cache
- `node`: inspect or manage the stable local `node_id`
- `site`: manage local release, session, and assignment state at the site store

## Common examples

```bash
deck init --out ./demo
deck completion bash > ./deck.bash
deck validate --file ./demo/workflows/apply.yaml
deck validate --file ./demo/workflows/prepare.yaml
deck validate control-plane-bootstrap

cd ./demo
deck prepare --out ./bundle.tar
deck plan --file ./workflows/apply.yaml
deck doctor --file ./workflows/apply.yaml --out ./reports/doctor.json
deck apply --file ./workflows/apply.yaml
```

Optional site-local inspection example:

```bash
deck server set http://127.0.0.1:8080 --api-token deck-site-v1
deck server up --root ./bundle --addr :8080
deck server scenarios --server http://127.0.0.1:8080
deck server health
deck apply --session session-1
```

## Notes

- `prepare` expects a workflow directory containing `prepare.yaml`, `apply.yaml`, and `vars.yaml`.
- scenario entrypoints live under `workflows/scenarios/`
- workflow imports resolve from `workflows/components/` using component-relative paths
- `validate` also accepts a bare scenario name and resolves it under `workflows/scenarios/`
- `apply` defaults to the `install` phase when phases are used.
- Help text is shown on stdout only when you request it with `--help` or `help`.
- Command and flag errors are written to stderr without automatic usage output.
- Prefer typed step kinds for common host changes.
- Keep `Command` for cases where the clearer typed form does not exist yet.
