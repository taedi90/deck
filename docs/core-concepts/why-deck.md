# Why deck?

`deck` started from a narrow but recurring operational problem: air-gapped Linux maintenance work. When you can't assume internet access, when SSH is heavily restricted, and when your procedures have grown beyond what a simple shell script can safely handle, you need a different approach.

Shell scripts are easy to write and can run almost anywhere. However, they stop being reliable once a procedure becomes complex. Code review turns into reverse-engineering. Code reuse turns into copy-and-paste. The most important question—"What is this procedure actually trying to do?"—gets buried under implementation details.

`deck` exists to convert those fragile, Bash-driven procedures into verifiable, bundle-based workflows.

## The problem it solves

Most infrastructure tools are built for connected environments. They usually assume:

- Network access during execution
- A reachable control plane
- Remote transport such as SSH
- A heavy automation runtime pre-installed on the target machine

These assumptions make sense in a cloud environment, but they create massive friction in air-gapped data centers. In practice, operators face several recurring hurdles:

- Packaging and transferring the dependencies for a heavy automation tool is often harder than the actual maintenance work.
- Strict security policies mean SSH cannot be treated as the default execution path.
- Local image, package, or file distribution is a hard prerequisite before any meaningful change can happen.
- Because of these constraints, teams fall back to a growing collection of shell scripts that are easy to start but impossible to maintain.

`deck` was built specifically for these disconnected environments.

## How deck addresses this

`deck` splits the workflow into distinct, manageable phases:

- **Preparation** happens in a connected environment.
- The **handoff** to the air-gapped network is an explicit, self-contained bundle.
- **Execution** happens locally on the target node.
- The **workflow** itself remains readable and reviewable as it grows.

Instead of treating artifact gathering and host mutation as a single blurred process, `deck` strictly separates them (`prepare`, `bundle`, `apply`). 

It also replaces large procedural scripts with typed workflow steps. A typed `InstallPackage` step immediately communicates its intent, whereas a shell block full of `yum` or `apt` commands hides the goal behind syntax.

## Core principles

- **Local-first**: The normal execution path is local to the machine being changed. No remote orchestration needed.
- **Bundle-first**: The workflow, the prepared artifacts, and the `deck` binary itself travel together across the air gap as a single package.
- **Readable**: Workflows make your intent visible. The reader shouldn't have to wade through implementation noise to understand what will change.
- **Manual-first**: The tool is designed for explicit, operator-driven maintenance sessions rather than unattended, continuous reconciliation.
- **Small surface area**: Typed steps and CLI commands are kept simple and obvious.
- **Single-binary by default**: On the site side, the entire operation relies solely on the standalone `deck` binary and the bundle contents. There is no background service or large runtime to install.

## What it improves

At a practical level, `deck` reduces:

- The high cost of reviewing long, complex shell procedures.
- The packaging overhead of heavyweight automation tools.
- The operational friction of assuming remote transport that doesn't exist.
- The ambiguity of mixing artifact gathering and host configuration.

It does not magically remove the complexity of your operational tasks. Instead, it captures that complexity in a form that is easy to review, package, transport, and run safely.

For a deeper dive into the system-level design behind these choices, see [Architecture](architecture.md).
