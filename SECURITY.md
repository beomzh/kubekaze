# Security Policy

English | [한국어](SECURITY.ko.md)

> Status: pre-release design. The model below is a commitment for the first release, not a description of shipped code.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Use GitHub's private reporting: **Security → Report a vulnerability** on this repository. Include the version, environment (distribution, Kubernetes version), and reproduction steps. We aim to acknowledge within 5 business days and to ship a fix or mitigation for confirmed issues as fast as practical, crediting reporters who want credit.

## Supported versions

Only the latest minor release receives security fixes until 1.0; after 1.0, the latest two minor releases.

## Security model

| Area | Commitment |
|------|-----------|
| Credentials | Uses your existing kubeconfig and its exec auth plugins. Never stores, copies, logs, or transmits credentials or tokens. |
| Read-only default | `status`, `why`, `logs`, `changes` perform only get/list/watch and log reads. |
| Mutating actions | `sh` (pods/exec), ephemeral debug containers, and similar actions are explicit, named in output, and ask for confirmation on contexts flagged as production. |
| Bulk delete | `rm` always shows a preview first and deletes only after confirmation; large counts or production-flagged contexts require typing the count/name; a safety cap applies even with `--yes`; per-object results are reported. |
| Kubeconfig writes | `ctx` / `ns` change only `current-context` and the context's `namespace` field, via atomic write with file mode preserved. Nothing else in the kubeconfig is touched. |
| Secrets | Secret `data` is never printed. Env values sourced from Secrets are masked. ConfigMap/Secret changes are reported as hashes only. |
| Log redaction | Common token and password patterns are masked in output by default; `--no-redact` must be passed explicitly. |
| Telemetry | None. No network calls except to the Kubernetes API servers you target (and to an LLM endpoint only if you enable the optional AI feature). |
| AI feature | Off by default. Sends only redacted, user-previewable (`--dry-run`) data to an endpoint you configure; supports local models. |
| Debug image | The ephemeral debug container image is configurable and pinned by digest by default; no `latest`. |
| Least privilege | Required RBAC per command is documented; missing permissions are reported, not worked around. |
| Terminal safety | Control characters in cluster-provided strings (events, logs, names) are escaped to prevent terminal escape-sequence injection. |
| Multi-cluster | Fan-out runs per context with that context's own credentials; results are never mixed across credentials. |

## Supply chain

- Releases are built in GitHub Actions from tagged commits, with actions pinned by commit SHA.
- Artifacts ship with SHA-256 checksums, a signed checksum file (cosign keyless), an SBOM (SPDX), and SLSA build provenance.
- Dependencies are tracked by Dependabot and scanned with `govulncheck`.
- Reproducible builds: `-trimpath`, static binary (`CGO_ENABLED=0`), version info injected via ldflags.

## Verifying a release

Instructions will be added to the release notes of each version (checksum + `cosign verify-blob`).
