# Why deck

`deck` started from a narrow but recurring operational problem: air-gapped Linux maintenance work where internet access could not be assumed, SSH was not always available, and the procedures had already grown beyond what a long shell script could comfortably carry.

Shell remains the easiest thing to run almost anywhere, but it stops being easy to trust once a procedure becomes large. Review turns into reverse-engineering. Reuse turns into copy-paste. The important question - what is this procedure trying to do? - gets buried under implementation details.

`deck` exists to give those procedures a clearer shape.

## The problem it tries to solve

Most infrastructure tools are built for connected environments. They usually assume some combination of:

- network access during execution
- a reachable control plane
- remote transport such as SSH
- a larger automation runtime already installed on the target machine

Those assumptions are sensible in many environments, but they become friction in air-gapped work.

In practice, a few specific problems kept repeating:

- packaging and transferring dependency-heavy automation runtimes could be harder than packaging the actual procedure
- some environments were restrictive enough that SSH could not be treated as the default execution path
- some environments required local image, package, or file distribution before the main workflow could succeed
- the fallback was usually a growing collection of shell scripts that were easy to start with and hard to maintain later

None of this makes existing tools wrong. It just means their default shape was often a poor fit for the environments `deck` was meant to serve.

## The response

`deck` takes a simpler position.

- preparation happens in a connected environment
- execution happens locally on the target node
- the offline handoff is an explicit bundle
- the workflow should stay reviewable as it grows

That is why `deck` splits work into `prepare`, `bundle`, and `apply` instead of treating artifact gathering and host mutation as one blurred process.

It is also why the project prefers typed workflow steps over large procedural scripts. A typed `Packages` step says more, and says it more clearly, than a shell block full of package-manager commands.

## What kind of tool it is

`deck` is not trying to be a full IaC platform or a general control plane.

It is a structured workflow tool for operational procedures, especially in environments where explicit handoff, local execution, and predictable packaging matter more than broad platform coverage.

The project aims to sit somewhere between a shell script and a larger automation system:

- more structured and reviewable than ad hoc scripts
- smaller and easier to carry than a dependency-heavy orchestration stack
- better suited to disconnected, staged, operator-driven work

## Core principles

- **Local-first**: the normal execution path is local to the machine being changed
- **Bundle-first**: the workflow, prepared artifacts, and binary travel together across the air gap
- **Readable**: workflows should make intent visible without forcing the reader through implementation noise
- **Manual-first**: the tool is designed for explicit operator-driven maintenance sessions, not unattended reconciliation
- **Small surface area**: typed steps and CLI commands are intentionally kept simple so the common path stays obvious
- **Single-binary by default**: on the site side, the normal operating model is built around the `deck` binary and the bundle contents rather than a larger installed runtime

That last point matters. `deck` deliberately avoids creating too many equivalent ways to express the same operational task. Once a tool allows the same thing to be modeled in several overlapping shapes, the cost shows up everywhere: in reviews, in examples, in docs, and in day-to-day operator hesitation.

## What it tries to improve

At a practical level, `deck` tries to reduce a few recurring costs:

- the review cost of long shell procedures
- the packaging cost of heavyweight automation runtimes
- the operational cost of assuming remote transport that may not exist
- the ambiguity that appears when artifact gathering and host mutation are mixed together

It does not remove complexity from the work itself. It tries to move that complexity into a form that is easier to review, package, transfer, and run.

## Who it is for

`deck` is for operators and engineers who already think in terms of staged procedures, artifacts, and repeatable maintenance tasks, but need a smaller and more explicit tool for disconnected or constrained environments.

For the system-level design behind those choices, see `docs/concepts/architecture.md`.
