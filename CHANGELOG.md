# Changelog

English | [한국어](CHANGELOG.ko.md)

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com), versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.1.1] - 2026-11-19

### Added
- `kubekaze update`: update the binary online (`--check`, `--version`), from an internal mirror (`--url`, `KUBEKAZE_UPDATE_URL`) or from a downloaded archive without any network (`--from`). Checksum-verified, sanity-run before replacing, atomic replacement, refuses Homebrew/krew installs and plain http. No automatic update checks.
- `why node/<name>`: node conditions, heartbeat, taints, events and unhealthy pods on the node, with ranked causes (kubelet down, disk/memory/PID pressure, CNI or runtime problems, cordon).
- `completion --alias <name>` and `completion install --alias <name>`: tab completion for a shell alias such as `kk`.
- `ctx` checks the API server and the login right after switching and warns about an expired login or an unreachable cluster (`--no-check` skips it).
- `ctx` picker groups contexts that share a cluster and user (what `oc project` creates per namespace); `--all` lists every context.
- Detailed `--help` for every command: what is checked, how targets work, safety rules, many examples, exit codes and environment variables.

### Changed
- `status` always checks nodes (not only with `-A`), reports a missing heartbeat as `Unknown`, and shows the kubelet's reason for NotReady.

## [0.1.0] - 2026-11-19

First release.

### Added
- CLI with `status`, `why`, `events`, `sh`, `logs`, `rm`, `ctx` and `ns` commands, built on client-go and the user's kubeconfig.
- `status`: unhealthy-only view with exit code 3 when problems exist.
- `why`: timeline, owner chain, recent rollout and rule-based ranked suspected causes (OOMKilled, image pull, scheduling, probes, volumes, ...).
- `events`: cross-namespace events sorted by time, collapsed repeats, filters and per-namespace summary.
- `sh`: shell probing (bash/sh/ash/busybox), workload targets, one-off commands, unprivileged ephemeral debug container pinned by digest.
- `logs`: multi-pod tail with automatic previous-run logs, error highlighting and secret masking.
- `rm`: bulk pod deletion with preview, UID preconditions, confirmation, production guard and `--max` cap.
- `ctx` / `ns`: context and namespace switching with previous (`-`); OpenShift projects.
- Shell completion for bash, zsh, fish and PowerShell (`kubekaze completion`, `kubekaze completion install`) with live completion of contexts, namespaces, pods, workloads and flag values.
- Grouped `--help` with examples for every command; "Did you mean" hints for mistyped commands and flags; exit code 2 and a usage line for wrong command lines.
- `kubekaze version` command.
- Terminal escape-sequence sanitising of cluster-provided text.
- Build and release setup: GoReleaser (linux/macOS/windows x amd64/arm64), CI matrix, govulncheck, Dependabot; GitHub Actions pinned by commit SHA.
- Project concept and design docs, English and Korean README, security policy, Apache-2.0 license and NOTICE.
