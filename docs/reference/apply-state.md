# Apply State

`deck apply` stores progress in a state file derived from the workflow `StateKey`.

## What identifies saved state

- resolved workflow bytes after imports expand
- effective vars for the run

This keeps state isolated by the final workflow fingerprint and input vars.

## Phase-based resume

Apply now resumes at phase boundaries, not step boundaries.

- completed phases are skipped on later non-fresh runs
- a failed phase is rerun from its first step on the next non-fresh run
- partial progress from inside a failed phase is not reused

## What is stored

- current phase name
- completed phase names
- failed phase name and error, when the run stops
- runtime vars exported by fully completed phases

## Parallel batches inside a phase

When a phase uses explicit `parallelGroup` batches:

- steps in the same batch start from the same `runtime` snapshot
- `register` outputs from that batch become visible only after the full batch succeeds
- if any step in the batch fails, the whole phase remains incomplete

## `--fresh`

Use `--fresh` to ignore saved apply state for that invocation.

- `deck apply --fresh` reruns all phases and writes fresh state back to the normal path
- `deck plan --fresh` shows a fresh-run view with no completed-phase skips or saved runtime vars

`--fresh` does not delete the state file before execution.
