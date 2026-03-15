# Why deck

`deck` grew out of a specific situation: air-gapped Kubernetes operations where SSH-driven tooling was not available, internet access was not assumed, and the shell scripts running maintenance procedures had grown large enough that reviewing them before execution was genuinely hard.

The problem was not just that shell is fragile. It was that a long shell file hides what the procedure actually does. Intent gets buried inside implementation. Reviews become reverse-engineering sessions. Reuse turns into copy-paste.

`deck` gives those procedures a cleaner shape. A typed `Packages` step says more than a block of `apt install` commands. A named phase boundary makes the procedure easier to scan. `deck lint` catches structural mistakes before the bundle leaves the connected environment. The bundle itself travels with the workflow and the artifacts it needs — no implicit dependencies, no reach-back to external services at run time.

## How it fits together

The operating model has two parts: preparation happens in a connected environment where packages, images, and files can be fetched; execution happens locally on the target machine inside the air gap. The `prepare` workflow handles the first part, the `apply` workflow handles the second, and `deck bundle build` packages everything needed to cross the boundary.

This separation is intentional. The operator on the far side of the air gap should be able to run `deck apply` without resolving external dependencies, contacting a control plane, or interpreting a long shell script.

## Design principles

- **Local-first**: the default path is local execution on the machine that needs the change, with no long-lived controller required.
- **Bundle-first**: workflow, artifacts, and the `deck` binary travel together so the offline handoff is explicit and complete.
- **Readable**: typed steps and named phases keep the procedure scannable as it grows.
- **Pragmatic**: `Command` is available for the edges that are not modeled yet, but it should not be the dominant authoring style.

## Who it's for

`deck` is for operators and engineers who already think in YAML, stages, and repeatable procedures, but need a smaller tool for disconnected or constrained work — somewhere between a shell script and a full automation platform.
