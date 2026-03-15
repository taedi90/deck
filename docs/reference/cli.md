# CLI Reference

The `deck` CLI is intentionally small.

It supports a simple operator flow: author the workflow, lint it, prepare bundle contents, build the bundle, and run locally.

## Default local flow

- `init`: create starter workflow files under `workflows/`
- `completion`: generate shell completion for bash, zsh, fish, and PowerShell
- `lint`: validate a workflow file or workspace against the workflow and step schemas
- `prepare`: gather artifacts into `outputs/`, refresh the local `deck` binary, and write `.deck/manifest.json`
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
deck lint --file ./demo/workflows/scenarios/apply.yaml
deck lint --file ./demo/workflows/scenarios/prepare.yaml

cd ./demo
deck prepare
deck bundle build --out ./bundle.tar
deck plan --file ./workflows/scenarios/apply.yaml
deck doctor --file ./workflows/scenarios/apply.yaml --out ./reports/doctor.json
deck apply --file ./workflows/scenarios/apply.yaml
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

- `prepare` expects a workflow tree rooted at `workflows/` with entrypoints under `workflows/scenarios/`.
- scenario entrypoints live under `workflows/scenarios/`
- workflow imports resolve from `workflows/components/` using component-relative paths
- `apply` runs all phases by default when phases are used; `--phase` narrows execution to one phase.
- `bundle build` archives the canonical workspace bundle inputs: `deck`, `workflows/`, `outputs/`, and `.deck/manifest.json`, and respects `.deckignore` within those paths.
- Help text is shown on stdout only when you request it with `--help` or `help`.
- Command and flag errors are written to stderr without automatic usage output.
- Prefer typed step kinds for common host changes.
- Keep `Command` for cases where the clearer typed form does not exist yet.
