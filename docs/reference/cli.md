# CLI Reference

The `deck` CLI is intentionally small.

It supports a simple operator flow: author the workflow, lint it, prepare bundle contents, build the bundle, and run locally.

## Default local flow

- `init`: create starter workflow files under `workflows/`
- `list`: list available scenarios from the local workspace or a saved server
- `completion`: generate shell completion for bash, zsh, fish, and PowerShell
- `lint`: validate a workflow file or workspace against the workflow and step schemas
- `prepare`: gather artifacts into `outputs/`, refresh the local `deck` binary, and write `.deck/manifest.json`
- `plan`: inspect which apply steps would run or skip before execution
- `apply`: execute the `apply` workflow locally

## Optional site-local helpers

- `server up`: expose a prepared bundle root over HTTP inside the air gap when a shared local source is useful
- `server down`: stop a daemonized local server started with `deck server up -d`
- `server set`: save the default server URL used for server-backed scenario lookup
- `server show`: show the saved default server URL
- `server unset`: clear the saved default server URL
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

## Common examples

```bash
deck init --out ./demo
deck list --source local
deck completion bash > ./deck.bash
deck lint --file ./demo/workflows/scenarios/apply.yaml
deck lint --file ./demo/workflows/scenarios/prepare.yaml

cd ./demo
deck prepare
deck bundle build --out ./bundle.tar
deck plan --scenario apply --source local
deck apply --scenario apply --source local
```

Optional site-local helper example:

```bash
deck server set http://127.0.0.1:8080
deck list --source server
deck server up --root ./bundle --addr :8080
deck server health --server http://127.0.0.1:8080
deck plan --scenario apply --source server
```

## Notes

- `prepare` expects a workflow tree rooted at `workflows/` with entrypoints under `workflows/scenarios/`.
- scenario entrypoints live under `workflows/scenarios/`
- `plan` and `apply` accept `--scenario` for named scenarios and `--workflow` for an explicit path or URL.
- `--source` controls whether `--scenario` resolves from the local workspace or the saved server.
- workspace-local metadata stays under `./.deck/`, while user-global config, state, cache, and run history use standard XDG locations.
- phase imports resolve from `workflows/components/` using component-relative paths
- `apply` runs all phases by default when phases are used; `--phase` narrows execution to one phase.
- `bundle build` archives the canonical workspace bundle inputs: `deck`, `workflows/`, `outputs/`, and `.deck/manifest.json`, and respects `.deckignore` within those paths.
- Help text is shown on stdout only when you request it with `--help` or `help`.
- Command and flag errors are written to stderr without automatic usage output.
- Prefer typed step kinds for common host changes.
- Keep `Command` for cases where the clearer typed form does not exist yet.
