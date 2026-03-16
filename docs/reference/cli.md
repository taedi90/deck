# CLI Reference

The `deck` CLI is intentionally small.

It supports a simple operator flow: author the workflow, lint it, prepare bundle contents, build the bundle, and run locally.

## Default local flow

- `init`: create starter workflow files under `workflows/`
- `list`: list available scenarios from the local workspace or the saved remote source
- `completion`: generate shell completion for bash, zsh, fish, and PowerShell
- `lint`: validate a workflow file or workspace against the workflow and step schemas
- `prepare`: gather artifacts into `outputs/`, refresh the local `deck` binary, and write `.deck/manifest.json`
- `plan`: inspect which apply steps would run or skip before execution
- `apply`: execute the `apply` workflow locally

## Optional site-local helpers

- `source set`: save the default remote source URL used for server-backed scenario lookup
- `source show`: show the effective default remote source URL
- `source unset`: clear the saved default remote source URL
- `server up`: expose a prepared bundle root over HTTP inside the air gap when a shared local source is useful
- `server down`: stop a daemonized local server started with `deck server up -d`
- `server health`: check `/healthz` on an explicit or saved source URL
- `server logs`: read local server audit logs from file or journal

## Optional AI-ready authoring helper

- `ask`: experimental helper to draft, refine, or review workflows from the current workspace using an LLM-backed authoring assistant
- `ask auth set`: save `ask.provider`, `ask.model`, `ask.endpoint`, and `ask.apiKey` in XDG config
- `ask auth show`: show the effective ask config with a masked api key
- `ask auth unset`: clear saved ask config

`ask` is experimental and available only in AI-ready builds. Lite builds keep the command surface but return a clear unsupported-feature error.

`ask` uses LLM-first intent classification and route-specific prompts. Workflow generation only runs for authoring routes (`draft`/`refine`), while explain/review/question routes return answer-oriented responses.

When model access is unavailable, `ask` degrades explicitly instead of silently pretending to answer with full reasoning. `explain` falls back to a local structural summary of the target file, `review` falls back to local findings, and generation routes fail fast because local validation cannot replace model output.

OpenAI-compatible provider support currently targets:

- `openai`
- `openrouter`
- `gemini`

You can override `provider`, `model`, and `endpoint` per run, or save defaults with `ask auth set`.

`ask.logLevel` controls terminal diagnostics on stderr:

- `basic`: route and provider summary
- `debug`: `basic` plus the user command and MCP/LSP events
- `trace`: `debug` plus classifier/route system prompts and user prompts

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
deck source set http://127.0.0.1:8080
deck list --source server
deck server up --root ./bundle --addr :8080
deck server health --server http://127.0.0.1:8080
deck plan --scenario apply --source server

deck ask auth set --provider openai --model gpt-5.4 --endpoint https://api.openai.com/v1 --api-key "$DECK_ASK_API_KEY"
deck ask "rhel9 single-node kubeadm cluster scenario"
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
- `--source` controls whether `--scenario` resolves from the local workspace or the saved remote source.
- workspace-local metadata stays under `./.deck/`, while user-global config, state, cache, and run history use standard XDG locations.
- `ask` workspace context lives under `./.deck/ask/`, while saved ask auth/defaults live under `~/.config/deck/config.json` as the top-level `ask` object.
- `ask auth set --log-level trace` is the quickest way to see the effective `deck ask` command, MCP/LSP events, and prompt text in terminal logs.
- optional augmentation config can be defined under `ask.mcp` and `ask.lsp` in the same config file.
- optional MCP and LSP augmentation is disabled by default and degrades gracefully when configured tools are unavailable.
- phase imports resolve from `workflows/components/` using component-relative paths
- `apply` runs all phases by default when phases are used; `--phase` narrows execution to one phase.
- `bundle build` archives the canonical workspace bundle inputs: `deck`, `workflows/`, `outputs/`, and `.deck/manifest.json`, and respects `.deckignore` within those paths.
- Help text is shown on stdout only when you request it with `--help` or `help`.
- Command and flag errors are written to stderr without automatic usage output.
- Prefer typed step kinds for common host changes.
- Keep `Command` for cases where the clearer typed form does not exist yet.
- `deck ask` previews changes by default and only writes workflow files when `--write` is present.
- `--max-iterations` applies to generation routes (`draft`/`refine`) only; non-generation routes do not run repair loops.
