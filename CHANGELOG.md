# Changelog

English | [한국어](CHANGELOG.ko.md)

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com), versioning: [SemVer](https://semver.org).

## [Unreleased]

## [0.2.5] - 2026-11-27 (not released, no tag; ships with the next minor release, 0.3.0)

### Added
- `kubekaze changes [--since 1h] [-n|-A]`: what changed just before it broke, oldest first: Deployment, StatefulSet and DaemonSet rollouts (image and env/resource/command/probe differences for Deployments, never environment values), created objects, edits of ConfigMaps, Secrets, Services, Ingresses and workload specs with the field manager that made them, Helm release revisions, autoscaling, and node reboot / NotReady / cordon events. `-o json` and `--since` completion. Deleted objects and previous values cannot be shown.

## [0.2.4] - 2026-11-27 (not released, no tag; ships with the next minor release, 0.3.0)

### Changed
- `events` / `why`: events that differ only in throwaway IDs (container and layer IDs, restart attempt number) are collapsed into one row that shows the latest message. Different containers and image digests stay separate.
- `why node`: the restart burst says how many more *containers* are not listed.

### Added
- `why node` / `why nodes`: a failing container storage on the node (I/O error or failed overlay mount in a pod's status) is reported as a node problem.

## [0.2.3] - 2026-11-27 (not released, no tag; ships with the next minor release, 0.3.0)

### Changed
- `why node`: a restart burst now shows when the last of it ended (date, time and zone) and each container's exit code, reason and time, so the matching node logs can be found without extra `kubectl` calls.
- `why node`: a restart burst or reboot with nothing newer for more than 24 hours is shown under "Past events" and no longer counts as a current problem (the node stays healthy and the exit code is not 3). `why nodes` mentions past events of healthy nodes in one line each; `-o json` marks them with `"past": true`.
- `why node`: when control plane components restart together, the hint points at the API server and etcd instead of saying the pods have nothing to do with each other.

## [0.2.2] - 2026-11-27 (not released, no tag; ships with the next minor release, 0.3.0)

### Added
- Simplified Chinese and Japanese versions of the README, security policy and concept document, linked from every language and included (README) in release archives. The CHANGELOG and release guide stay in English and Korean; command output and `--help` stay in English.

## [0.2.1] - 2026-11-20 (not released, no tag; ships with the next minor release, 0.3.0)

### Changed
- `completion install --alias kk` and the help texts now say that `--alias` only wires up tab completion for a name you already have and does not create the command `kk`, and show how to define it (`alias kk=kubekaze` or a symlink). The root help line "also for an alias" read as if the alias were created.

## [0.2.0] - 2026-11-20

Includes everything from the unreleased 0.1.3 to 0.1.8: new `ns` summary, `ls`, `top`, node restart signals, and fixes found while testing on a real OpenShift cluster.

### Added
- `why node`: container restart history is now part of the diagnosis. Several unrelated pods whose containers restarted within 10 minutes of each other (last 7 days) are reported as a node-level event, with what to check inside the node (`last -x reboot`, `journalctl --list-boots`, `coredumpctl list`, `dmesg -T`), because Kubernetes cannot see kernel or runtime crashes and core dumps. When the node already has a problem, containers with 10 or more restarts in the last 7 days are listed worst first; they never make a healthy node look unhealthy.
- `kubekaze top`: read-only usage snapshot from the metrics API (metrics-server). Nodes with CPU and memory vs allocatable (highest first, above 80% in red) and the top pods (`--limit`, default 10) by `--sort cpu|memory` in the current namespace or with `-A`, shown with the percent of their limit so throttling or near-OOM pods stand out. `top ns` (or `--by-namespace`) sums usage per namespace. `-w/--watch [--interval 5s]` redraws in place on a terminal and prints plain repeated snapshots otherwise; Ctrl-C exits with 130. `-o json` and tab completion for `--sort`, `--limit`, `--interval`.
- When metrics-server is missing or the metrics API is forbidden, `top` prints one line saying what to install or which RBAC is needed and exits 1.
- `kubekaze ls`: one-screen overview of the current namespace (`-n`, `-A`): Deployments, StatefulSets and DaemonSets (ready/desired), Jobs, CronJobs, Services, Ingresses, OpenShift Routes (only when the cluster has them), PVC phases, and the names of ConfigMaps and Secrets. Unhealthy objects come first, large healthy sections are summarised with a count, and kinds you may not read are reported as skipped.
- `kubekaze ls <kind> [name...]` lists one kind in detail (`deploy`, `sts`, `ds`, `job`, `cronjob`, `svc`, `ingress`, `route`, `pvc`, `cm`, `secret`); tab completion offers kinds and object names. `-o json` is supported.
- Secrets are shown by name, type and key count only; their data is never printed.
- `ns`: the interactive picker shows, next to each namespace (or OpenShift project), the pod count, the number of unhealthy pods (same rules as `status`) and the number of Warning events from the last hour. Pods and events are listed once cluster-wide, not per namespace; filtering still matches the name. If you may not list them cluster-wide, a note says so and the plain name list is shown (`--no-summary` skips the calls).
- `ns --list` (`-l`) and `ns -o json`: the same summary non-interactively, for pipes and scripts. Plain `ns` piped still prints just the names.
- `why node`: a failing node-level network agent (ovnkube-node, calico-node, cilium, aws-node, flannel, multus, ...) is reported as the cause of pods stuck in `ContainerCreating`, with the failing pod to explain next; several pods stuck for over 5 minutes are reported as a node-level problem.

### Changed
- `why node` on a healthy node says that only the Kubernetes API was checked and where to look inside the node (`coredumpctl list`, `dmesg -T`).
- `why node`: a `Rebooted` event is shown for 7 days instead of 2 hours (ranked lower after the first 2 hours), because a reboot is a fact about the node's past and does not clear itself.
- `status` without `-A` says that only the current namespace and the nodes were checked, because "healthy" there says nothing about the rest of the cluster.

### Fixed
- `sh --debug -- <command>`: `--debug` was silently ignored and the command ran in the application's own container. It is now refused with a message, because a debug container is interactive.
- `why <pod>`: a `CreateContainerError` caused by the node's container storage (for example `creating overlay mount ... input/output error`) was reported as "missing ConfigMap/Secret". It is now reported as a failing node filesystem, with what to check on the node. A real missing ConfigMap/Secret is still reported as before.
- `why` / `events`: very long event and evidence messages (runtime errors can exceed 1000 characters) are cut at 240 characters in tables. `-o json` keeps the full text.
- `why <pod>`: log lines are quoted only as evidence for the container they were read from, not for another container of the same pod.
- `completion --alias` (bash): tab completion did nothing for `kk` unless `kk` was also a defined shell alias, because the script ran `kk __complete` and bash then fell back to file names. It now asks `kubekaze` itself.
- `why node` / `status`: a Ready node was reported as silent when its heartbeat was a few minutes old. The kubelet only rewrites the node status about every 5 minutes (liveness is a Lease), so the limit is now 10 minutes.
- `why node`: warnings older than 2 hours (OpenShift keeps events for days) are no longer treated as causes, and an `ImageGCFailed` "container not found" message is recognised as harmless noise instead of a disk problem.

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
