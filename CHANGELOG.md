# Changelog

English | [한국어](CHANGELOG.ko.md)

All notable changes are documented here. Format: [Keep a Changelog](https://keepachangelog.com), versioning: [SemVer](https://semver.org).

## [Unreleased]

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
- Terminal escape-sequence sanitising of cluster-provided text.
- Build and release setup: GoReleaser (linux/macOS/windows x amd64/arm64), CI matrix, govulncheck, Dependabot; GitHub Actions pinned by commit SHA.
- Project concept and design docs, English and Korean README, security policy, Apache-2.0 license and NOTICE.
