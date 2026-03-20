# deck

<img align="right" src="assets/logo.png" width="120" alt="logo">

[Korean README](./README.ko.md) | [Documentation](./docs/README.md)

**Structured workflows for air-gapped operations : prepare, bundle, and apply in one binary!**

<br clear="right" />

## What is deck?

Operational procedures for disconnected sites—such as Kubernetes bootstraps, package installations, and host configuration—often start as shell scripts. Over time, these scripts grow until they become too large and complex to review confidently. 

`deck` provides a cleaner, structured alternative. It replaces fragile Bash scripts with typed steps, validates your workflows before execution, and packs everything needed into a self-contained bundle that can be securely transported and run locally on the target machine.

## Quick Start

Create a workflow, validate it, bundle it, and run it on your target machine.

```bash
# 1. Initialize a new demo project
deck init --out ./demo

cd ./demo

# 2. Validate the generated workflows
deck lint

# 3. Prepare artifacts defined in the workflows
deck prepare

# 4. Build a self-contained bundle
deck bundle build --out ./bundle.tar

# 5. Run the workflow locally on the target machine
deck apply
```

For a detailed walkthrough, start with the [Quick Start Guide](docs/getting-started/quick-start.md).

## Core Features

- **Typed steps**: File, Package, ManageService — step kinds that make intent visible without reading through shell syntax.
- **Pre-flight validation**: Lint catches schema errors before you carry the bundle into the site.
- **Self-contained bundle**: Workflows, artifacts, and the `deck` binary in a single archive. No dependency surprises on site.
- **Air-gap native**: No SSH, no control plane, no internet access assumed at execution time.

## Installation

Requirements:

- Go 1.25+ (Any OS for build and prepare)
- Linux target environment (RHEL, Ubuntu) for the `apply` step

```bash
# Build and install from source
go install ./cmd/deck

# Verify
deck version
```

### Shell Completion

To enable shell completion in your current session, run:

```bash
source <(deck completion bash) # for bash
source <(deck completion zsh)  # for zsh
deck completion fish | source  # for fish
```

To make it persistent, add the relevant command to your shell's startup file (e.g., `~/.bashrc` or `~/.zshrc`). For detailed instructions for all supported shells, see the [CLI Reference](docs/reference/cli.md#shell-completion).

## Documentation

- [Getting Started](docs/getting-started/README.md)
- [Core Concepts](docs/core-concepts/README.md)
- [User Guide](docs/user-guide/README.md)
- [Reference](docs/reference/README.md)
- [Contributing](docs/contributing/README.md)

## License

Apache-2.0. See `LICENSE`.
