# kubekaze

[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
![Status](https://img.shields.io/badge/status-concept-orange)

English | [한국어](README.ko.md)

**A terminal-first Kubernetes companion that tells you what is wrong and why, in one command.**

> **Status: concept stage.** There is no installable release yet. This README describes the intended behavior; see [docs/CONCEPT.md](docs/CONCEPT.md).

## Why

- k9s is great for browsing, but it is a resident UI, not something you pipe or paste into a ticket.
- stern tails logs, but does not explain anything.
- krew plugins are small and single-purpose; you still stitch the answers together.
- Daily friction: Does this image have `bash` or `sh`? Was it `logs -p`? Which context am I on?

## Planned commands

```text
kubekaze status                 # only what is unhealthy, in this namespace/cluster
kubekaze why pod/api-7d9f       # correlated timeline + ranked suspected causes
kubekaze events -A --type Warning   # events sorted by time across namespaces; --summary per namespace
kubekaze sh deploy/api          # finds a working shell (bash/sh/ash), or a debug container
kubekaze logs deploy/api        # multi-pod tail, crash-aware (previous logs included)
kubekaze rm pods --status CrashLoopBackOff,Evicted   # bulk delete with preview + confirmation
kubekaze ctx [name]             # switch context (fuzzy list, or direct)
kubekaze ns [name|-]            # switch namespace; projects on OpenShift; "-" = previous
kubekaze changes --since 1h     # what changed just before it broke (later release)
```

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
