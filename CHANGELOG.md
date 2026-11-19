# Changelog

English | [한국어](CHANGELOG.ko.md)

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com), versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.1.6] - 2026-11-19 (not released, no tag; ships with the next minor release, 0.2.0)

### Added
- `kubekaze top`: read-only usage snapshot from the metrics API (metrics-server). Nodes with CPU and memory vs allocatable (highest first, above 80% in red) and the top pods (`--limit`, default 10) by `--sort cpu|memory` in the current namespace or with `-A`, shown with the percent of their limit so throttling or near-OOM pods stand out. `top ns` (or `--by-namespace`) sums usage per namespace. `-w/--watch [--interval 5s]` redraws in place on a terminal and prints plain repeated snapshots otherwise; Ctrl-C exits with 130. `-o json` and tab completion for `--sort`, `--limit`, `--interval`.
- When metrics-server is missing or the metrics API is forbidden, `top` prints one line saying what to install or which RBAC is needed and exits 1.

## [0.1.4] - 2026-11-19 (not released, no tag; ships with the next minor release, 0.2.0)

### Added
- `ns`: the interactive picker shows, next to each namespace (or OpenShift project), the pod count, the number of unhealthy pods (same rules as `status`) and the number of Warning events from the last hour. Pods and events are listed once cluster-wide, not per namespace; filtering still matches the name. If you may not list them cluster-wide, a note says so and the plain name list is shown (`--no-summary` skips the calls).
- `ns --list` (`-l`) and `ns -o json`: the same summary non-interactively, for pipes and scripts. Plain `ns` piped still prints just the names.

## [0.1.3] - 2026-11-19 (not released, no tag; ships with the next minor release, 0.2.0)

### Fixed
- `completion --alias` (bash): tab completion did nothing for `kk` unless `kk` was also a defined shell alias, because the script ran `kk __complete` and bash then fell back to file names. It now asks `kubekaze` itself.
- `why node` / `status`: a Ready node was reported as silent when its heartbeat was a few minutes old. The kubelet only rewrites the node status about every 5 minutes (liveness is a Lease), so the limit is now 10 minutes.
- `why node`: warnings older than 2 hours (OpenShift keeps events for days) are no longer treated as causes, and an `ImageGCFailed` "container not found" message is recognised as harmless noise instead of a disk problem.

### Added
- `why node`: a failing node-level network agent (ovnkube-node, calico-node, cilium, aws-node, flannel, multus, ...) is reported as the cause of pods stuck in `ContainerCreating`, with the failing pod to explain next; several pods stuck for over 5 minutes are reported as a node-level problem.

## [0.1.2] - 2026-11-19

### Added
- `why node` (or `nodes`) checks every node and prints only the ones with problems, plus a summary of the healthy ones. It used to be read as a pod named "node".
- Tab completion for `why` offers `node/`, `nodes` and node names; `job/` and job names are offered for `why`, `sh` and `logs`.

### Fixed
- `completion install --alias kk` did not work in a new bash or fish shell, because both load completion files by the name you type. It now also writes a file named after the alias (bash: `.../completions/kk`, fish: `kk.fish`).
- `completion install --help` and the README list which file is written for each shell.

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
