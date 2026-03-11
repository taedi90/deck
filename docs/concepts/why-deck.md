# Why deck

`deck` exists for a narrow problem.

In connected environments, established tools already cover a lot of ground. `deck` does not try to outgrow or out-market them. It focuses on the places they do not fit well: disconnected sites, constrained operations, and procedures that still need local human execution.

## The real problem

The problem is not only that Bash can be dangerous.

The bigger problem is that Bash-based procedures often collapse as they grow:

- the intent of the procedure gets buried inside implementation details
- reviews get harder because everything looks like shell
- reuse becomes informal copy-paste
- validation usually happens late
- operators have to reverse-engineer the script before trusting it

`deck` gives those procedures a smaller and clearer shape.

## What deck does

- models maintenance work as YAML workflows with steps and phases
- prefers typed operations for common host changes
- validates workflow structure before transport or execution
- builds a self-contained bundle for offline handoff
- runs locally on the target machine without requiring SSH or a long-lived controller

## Design principles

- **Simple**: small command surface and a bounded operating model
- **Local-first**: the default path is local execution on the machine that needs the change
- **Bundle-first**: bring the workflow and its required artifacts together
- **Readable**: operators should be able to review intent quickly
- **Pragmatic**: shell stays available through `RunCommand`, but it should not be the main authoring style

## What deck is not trying to be

- a replacement for broad infrastructure platforms
- a remote orchestration control plane
- a permanent site management service
- a tool that assumes rich online dependencies

## The intended user

`deck` is for operators and DevOps engineers who already think in YAML, stages, manifests, and repeatable procedures, but need a much smaller tool for disconnected work.
