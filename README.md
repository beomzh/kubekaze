# kubekaze

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Status](https://img.shields.io/badge/status-early%20development-orange)

English | [한국어](README.ko.md)

**A terminal-first Kubernetes companion that tells you what is wrong and why, in one command.**

> **Status: early development (v0.1).** The core commands below work and were tried on a kind cluster (bulk delete is so far covered by unit tests only). Expect rough edges and changes between 0.x releases. See [Install](#install). Design background: [docs/CONCEPT.md](docs/CONCEPT.md).

## Why

- k9s is great for browsing, but it is a resident UI, not something you pipe or paste into a ticket.
- stern tails logs, but does not explain anything.
- krew plugins are small and single-purpose; you still stitch the answers together.
- Daily friction: Does this image have `bash` or `sh`? Was it `logs -p`? Which context am I on?

## Commands

| Command | What it does | State |
|---------|--------------|-------|
| `kubekaze status` | Only what is unhealthy: nodes (always checked), workloads, pods, jobs, PVCs. Exit code 3 when problems exist | available |
| `kubekaze why <pod\|deploy/x\|node/x>` | Timeline, evidence and ranked suspected causes for a pod, a workload or a node | available |
| `kubekaze events -A --type Warning` | Events sorted by time across namespaces; `--summary` per namespace | available |
| `kubekaze sh <pod\|deploy/x>` | Finds a working shell (bash/sh/ash), or an ephemeral debug container | available |
| `kubekaze logs <pod\|deploy/x>` | Multi-pod tail; previous run of restarted containers included | available |
| `kubekaze rm pods --status CrashLoopBackOff,Evicted` | Bulk delete with preview and confirmation | available |
| `kubekaze ctx [name\|-]` | Switch context; groups `oc` per-namespace contexts; warns right away about an expired login | available |
| `kubekaze ns [name\|-]` | Switch namespace; projects on OpenShift | available |
| `kubekaze changes --since 1h` | What changed just before it broke | planned |
| `--all-contexts` / `--context 'prod-*'` | Run across several clusters | planned |
| AI summary (opt-in, local models supported) | Plain-language explanation | planned |

Examples:

```text
kubekaze status -A
kubekaze why deploy/api
kubekaze why node/worker-3
kubekaze sh deploy/api -c sidecar
kubekaze sh api-7d9f -- ls -l /app
kubekaze logs deploy/api -f --since 10m
kubekaze events -A --summary
kubekaze rm pods -A --status Evicted --dry-run
```

Exit codes: `0` ok, `1` error, `2` wrong command, flag or argument, `3` problems found (`status`, `why`). For `sh`, the remote command's exit code is passed through.

Global flags: `--kubeconfig`, `--context`, `-n/--namespace`, `-A/--all-namespaces`, `-o text|json`.

## Install

Download the archive for your OS and CPU (Linux, macOS, Windows; amd64 or arm64) from the [Releases page](https://github.com/beomzh/kubekaze/releases), check it against `checksums.txt`, and put the `kubekaze` binary on your `PATH`.

Or build from source (Go 1.26 or newer):

```bash
git clone https://github.com/beomzh/kubekaze.git
cd kubekaze
make build        # produces ./bin/kubekaze
```

Release process and verification: [docs/RELEASING.md](docs/RELEASING.md). Homebrew and krew packages are planned.

## Shell completion

```bash
kubekaze completion install      # detects bash, zsh or fish from $SHELL and writes one file in your home directory
kubekaze completion bash         # or print the script: bash, zsh, fish, powershell
```

Using `alias kk=kubekaze`? Add `--alias kk` (`kubekaze completion install --alias kk`), because shells bind completion to the command name.

Completes commands, flags, contexts, namespaces, pods and workloads (`deploy/...`), looked up live from the cluster you point at. Mistyped commands and flags get a "Did you mean" hint, and every command has a detailed description and examples in `--help` (also `kubekaze --help` for exit codes and environment variables).

## Works with

Standard kubeconfig and client-go auth, so the same tool works on VM/kubeadm clusters, EKS, AKS, GKE, OpenShift, and Rancher-managed clusters. Multi-cluster fan-out (`--context 'prod-*'`) is planned.

## Platforms

A single self-contained binary (no `kubectl`, `oc`, or shell needed) for Linux, macOS, and Windows, on both amd64 and arm64.

## Principles

Read-only by default · no agent, no server, no telemetry · least-privilege RBAC · secret values never printed · pipe-friendly (`-o json`) · rules first, optional AI.

## Security

See [SECURITY.md](SECURITY.md). Report vulnerabilities privately via GitHub's "Report a vulnerability".

## Documentation

- [Concept](docs/CONCEPT.md)
- [Releasing and packaging](docs/RELEASING.md)
- [Security policy](SECURITY.md)

## License

Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE).

Kubernetes is a registered trademark of The Linux Foundation. This project is not affiliated with or endorsed by the Kubernetes project, Red Hat, AWS, Microsoft, Google, or SUSE.
