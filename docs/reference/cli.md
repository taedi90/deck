# CLI Reference

The `deck` CLI is intentionally small.

It supports a simple operator flow: author the workflow, lint it, prepare bundle contents, build the bundle, and run locally.

## Default local flow

- `init`: create starter workflow files under `workflows/`
- `lint`: validate a workflow file or workspace against the workflow and step schemas (`-o text|json`)
- `prepare`: gather artifacts into `outputs/`, write a local `deck` launcher, and write `.deck/manifest.json`
- `plan`: inspect which apply steps would run or skip before execution (`-o text|json`)
- `apply`: execute the `apply` workflow locally

## Additional helpers

- `list`: list available scenarios from the local workspace or the saved remote server
- `server remote set`: save the default remote server URL used for server-backed scenario lookup
- `server remote show`: show the effective default remote server URL
- `server remote unset`: clear the saved remote server URL
- `server up`: expose a prepared bundle root over HTTP inside the air gap when a shared local source is useful
- `server down`: stop a daemonized local server started with `deck server up -d`
- `server health`: check `/healthz` on an explicit server or the saved remote server URL (`-o text|json`)
- `server logs`: read local server audit logs from file or journal
- `version`: show the current `deck` build version and metadata (`-o text|json`)
- `completion`: generate shell completion for bash, zsh, fish, and PowerShell

## Authoring helper

- `ask`: experimental helper to draft, refine, or review workflows from the current workspace using an LLM-backed authoring assistant
- `ask config set`: save `ask.provider`, `ask.model`, `ask.endpoint`, and `ask.apiKey` in XDG config
- `ask config show`: show the effective ask config with a masked api key
- `ask config unset`: clear saved ask config

`ask` is experimental and ships as part of the standard `deck` binary.

For a task-oriented guide to configuring and using `deck ask`, see `../guides/ask.md`.

`ask` uses LLM-first intent classification and route-specific prompts. Workflow generation only runs for authoring routes (`draft`/`refine`), while explain/review/question routes return answer-oriented responses.

When model access is unavailable, `ask` degrades explicitly instead of silently pretending to answer with full reasoning. `explain` falls back to a local structural summary of the target file, `review` falls back to local findings, and generation routes fail fast because local validation cannot replace model output.

OpenAI-compatible provider support currently targets:

- `openai`
- `openrouter`
- `gemini`

You can override `provider`, `model`, and `endpoint` per run, or save defaults with `ask config set`.

`ask.logLevel` controls terminal diagnostics on stderr:

- `basic`: route and provider summary
- `debug`: `basic` plus the user command and MCP/LSP events
- `trace`: `debug` plus classifier/route system prompts and user prompts

These commands are additive. They do not replace the default local execution path.

`deck lint -o json` returns a structured report with the validated workflow list, summary counts, supported workflow contracts, and warning-level `findings` such as opaque `Command` steps or remote artifacts without integrity checks.

`deck plan -o json` returns the resolved workflow path, state path, runtime var keys, per-step actions, and a summary section.

`deck server health -o json` returns the resolved server URL, `/healthz` URL, and HTTP status.

`deck bundle verify -o json` returns the verified bundle path and final status.

`deck cache list -o json` and `deck server logs -o json` keep machine-readable output on stdout while `--v=<n>` sends path and count diagnostics to stderr.

Global `--v=<n>` writes diagnostics to stderr without changing stdout result contracts. Current levels follow this pattern:

- `--v=0`: result only
- `--v=1`: workflow/source/path decisions and high-level execution context
- `--v=2`: per-step apply diagnostics, plan evaluation details, and deeper bundle/prepare/health inspection counts
- `--v=3`: contract notes, lint finding hints, and the most detailed plan/lint traces

In practice:

- `deck plan --v=3` adds workflow/runtime var traces and per-step evaluation details
- `deck prepare --v=2` adds artifact group and cache reuse/fetch diagnostics
- `deck bundle build --v=2` and `deck bundle verify --v=2` add manifest entry breakdowns

## Shell completion

`deck completion` is the only completion entrypoint. Supported shells: `bash`, `zsh`, `fish`, `powershell`.

### Immediate sourcing

To enable completion for your current shell session:

```bash
source <(deck completion bash)
source <(deck completion zsh)
deck completion fish | source
```

### Persistent registration

To enable completion for all future shell sessions, add the sourcing command to your shell's initialization file:

- **Bash**: Add `source <(deck completion bash)` to `~/.bashrc`.
- **Zsh**: Add `source <(deck completion zsh)` to `~/.zshrc`.
- **Fish**: Create a file at `~/.config/fish/completions/deck.fish` containing `deck completion fish | source`.
- **PowerShell**: Add `deck completion powershell | Out-String | Invoke-Expression` to your `$PROFILE`.

## Other lifecycle commands

- `bundle`: bundle lifecycle operations
- `cache`: inspect or clean the artifact cache

## Common examples

```bash
deck init --out ./demo
deck version
deck version -o json
deck list --source local
deck completion bash > ./deck.bash
deck lint --file ./demo/workflows/scenarios/apply.yaml
deck lint --file ./demo/workflows/scenarios/apply.yaml -o json
deck lint --file ./demo/workflows/prepare.yaml

cd ./demo
deck prepare
deck prepare --bundle-binary-source local --bundle-binary-dir ../test/artifacts/bin --bundle-binary linux/amd64 --bundle-binary linux/arm64
deck prepare --bundle-binary-source release --bundle-binary-version v0.1.0 --bundle-binary linux/amd64
deck bundle build --out ./bundle.tar
deck plan --scenario apply --source local
deck plan --scenario apply --source local -o json
deck apply --scenario apply --source local
```

Prepare runtime binary notes:

- `prepare` always writes a root `./deck` launcher and stores real runtime binaries under `outputs/bin/<os>/<arch>/deck`
- `--bundle-binary` is repeatable and selects which runtime tuples land in the bundle
- `--bundle-binary-source=local` reads from the current executable or from `--bundle-binary-dir`
- `--bundle-binary-source=release` downloads matching GitHub Release archives and extracts `deck`
- `auto` defaults to `release` for tagged builds and `local` for `dev` builds

Optional site-local helper example:

```bash
deck server remote set http://127.0.0.1:8080
deck list --source server
deck server up --root ./bundle --addr :8080
deck server health --server http://127.0.0.1:8080
deck server health --server http://127.0.0.1:8080 -o json
deck server logs --root ./bundle --source file -o json --v=1
deck bundle verify --file ./bundle -o json
deck cache list -o json --v=1
deck plan --scenario apply --source server

deck ask config set --provider openai --model gpt-5.4 --endpoint https://api.openai.com/v1 --api-key "$DECK_ASK_API_KEY"
deck ask "create an air-gapped rhel9 single-node kubeadm workflow"
deck ask plan "air-gapped rhel9 kubeadm cluster with prepare/apply split"
deck ask "explain what workflows/scenarios/apply.yaml does"
deck ask --review
deck ask --write --from ./request.md
```

Optional ask augmentation config example:

```json
{
  "ask": {
    "provider": "openai",
    "model": "gpt-5.4",
    "logLevel": "trace",
    "mcp": {
      "enabled": true,
      "servers": [
        {
          "name": "context7",
          "command": "context7-mcp",
          "args": []
        }
      ]
    },
    "lsp": {
      "enabled": true,
      "yaml": {
        "command": "yaml-language-server",
        "args": ["--stdio"]
      }
    }
  }
}
```

## Notes

- `prepare` expects a workflow tree rooted at `workflows/` with entrypoints under `workflows/scenarios/`.
- scenario entrypoints live under `workflows/scenarios/`
- `plan` and `apply` accept `--scenario` for named scenarios and `--workflow` for an explicit path or URL.
- `plan` and `apply` support `--fresh` to ignore saved apply state for that invocation.
- `--source` controls whether `--scenario` resolves from the local workspace or the saved remote server.
- workspace-local metadata stays under `./.deck/`, while user-global config, state, cache, and run history use standard XDG locations.
- `ask` workspace context lives under `./.deck/ask/`, while saved ask config defaults live under `~/.config/deck/config.json` as the top-level `ask` object.
- `deck ask plan` writes plan artifacts under `./.deck/plan/` by default (`<timestamp>-<slug>.md`, `<timestamp>-<slug>.json`, `latest.md`, `latest.json`).
- `deck ask --from .deck/plan/<name>.md "implement this plan"` prefers the same-basename `.json` artifact when present.
- complex one-shot authoring requests may stop after planning if blockers remain; in that case `deck ask` prints the saved plan paths and follow-up commands instead of writing weak output.
- `ask config set --log-level trace` is the quickest way to see the effective `deck ask` command, MCP/LSP events, and prompt text in terminal logs.
- optional augmentation config can be defined under `ask.mcp` and `ask.lsp` in the same config file.
- optional MCP and LSP augmentation is disabled by default and degrades gracefully when configured tools are unavailable.
- phase imports resolve from `workflows/components/` using component-relative paths
- `apply` runs all phases by default when phases are used; `--phase` narrows execution to one phase.
- top-level `steps` execute as an implicit `default` phase.
- `parallelGroup` only parallelizes consecutive steps inside one phase.
- `bundle build` archives the canonical workspace bundle inputs: the root `deck` launcher, `workflows/`, `outputs/`, and `.deck/manifest.json`, and respects `.deckignore` within those paths.
- Help text is shown on stdout only when you request it with `--help` or `help`.
- Command and flag errors are written to stderr without automatic usage output.
- `version` prints `deck <version>` by default and supports `-o json` for machine-readable metadata.
- Prefer typed step kinds for common host changes.
- Keep `Command` for cases where the clearer typed form does not exist yet.
- `deck ask` previews changes by default and only writes workflow files when `--write` is present.
- `--max-iterations` applies to generation routes (`draft`/`refine`) only; non-generation routes do not run repair loops.

<!-- BEGIN GENERATED:ASK_CLI_CONTEXT -->
## Ask CLI context

- `deck ask` previews by default; add `--write` to write workflow files.
- `deck ask plan` saves reusable plan artifacts under `./.deck/plan/`.
<!-- END GENERATED:ASK_CLI_CONTEXT -->

<!-- BEGIN GENERATED:ASK_AUTHORING_CONTEXT -->
## Ask authoring context

- Top-level workflow authoring reference for deck workflows.
- Imports are only valid under phases[].imports and resolve from workflows/components/ using component-relative paths.
- Prefer workflows/vars.yaml for configurable values that would otherwise be repeated inline across steps or files.
- Prefer typed steps over `Command` when a typed step exists.
<!-- END GENERATED:ASK_AUTHORING_CONTEXT -->
