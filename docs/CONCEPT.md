# Concept

> **kubekaze** (風, "wind" in Japanese). English | [한국어](CONCEPT.ko.md)

## One-line pitch

A terminal-first Kubernetes companion that answers **"what is wrong, and why?"** in one command, and removes the daily friction of `exec`, `logs -p`, and context juggling.

## Where it sits

| Tool | What it is good at | Gap this project fills |
|------|--------------------|------------------------|
| k9s | Interactive browsing (TUI) | Not a command you can pipe, script, or paste into a ticket |
| stern | Multi-pod log tail | Only logs; no cause analysis |
| krew plugins | Many small single-purpose tools | You still stitch the results together by hand |
| kubectl | Everything, raw | Many commands and flags to reach one conclusion |

Design stance: **command in, answer out.** No resident UI. Output is readable by humans by default and machine-readable with `-o json`.

## Pillars

### 1. Triage (incident response)

- `status`: show only what is unhealthy in a namespace or cluster (CrashLoopBackOff, Pending, ImagePullBackOff, failed Jobs, NotReady nodes, PVCs near full, certificates near expiry). Healthy objects are hidden.
- `why <kind>/<name>`: one correlated timeline for an object: current state, events, last termination reason (OOMKilled, exit code), the tail of the previous container's log, the owner chain (Pod → ReplicaSet → Deployment), and what changed recently. Ends with ranked **suspected causes** produced by deterministic rules.
- `events`: events sorted the way you actually need them, which `kubectl get events` does not do by default.
  - `events -A` (all namespaces) or `events -n a,b` (several); default sort is by last-seen time, newest last, so the latest problem is at the bottom of your terminal.
  - `--sort time|count|type|namespace|object`, `--type Warning`, `--since 30m`, `--reason BackOff,FailedScheduling`, `--for pod/api`.
  - Repeated events are collapsed with a count (`×37 over 12m`) instead of flooding the screen; `--no-collapse` shows raw events.
  - `--summary` prints one line per namespace (warning count, top reasons), a quick "which namespace is on fire" view; `-w` follows new events live.
  - Note: Kubernetes keeps events for about an hour by default; the tool says so when the result may be incomplete.
- `changes --since 1h`: "what changed just before it broke?" (rollouts, ConfigMap/Secret revisions by hash only, scaling, node events).

### 2. Daily friction

- `sh <pod>`: no more guessing whether the image has bash or sh.
  1. Resolve the target: pod name, `deploy/x`, or label selector → pick a Running pod; container chosen via the `kubectl.kubernetes.io/default-container` annotation, otherwise ask.
  2. Probe candidate shells in order (`bash`, `sh`, `ash`, `busybox sh`) using non-interactive exec; open the first that works.
  3. If the image has no shell (distroless/scratch), offer an **ephemeral debug container** (`kubectl debug` semantics) with a configurable, pinned image.
  4. On OpenShift clusters, behave like `oc rsh` (same defaults) without requiring `oc`.
- `logs`: stern-style multi-pod tail, but crash-aware: if a container restarted, the previous container's log is included automatically (what `-p` does) and errors are highlighted.
- `top`: a read-only usage snapshot from the metrics API (metrics-server): nodes vs allocatable, the top pods by CPU or memory with usage as a percent of the limit (so throttling or near-OOM pods stand out), per-namespace totals (`top ns`) and a `-w` live view. Without metrics-server it says what to install instead of failing with a stack of errors.
- `rm`: bulk-delete pods (and other resources) without chaining `get | grep | awk | xargs kubectl delete`.
  - Select by name pattern, label, status (`--status CrashLoopBackOff,Error,Evicted,Completed`), age (`--older-than 2h`), node, or pick interactively from a list.
  - **Always previews first** (a dry run listing exactly what will be deleted); it deletes only after confirmation. Deleting many objects, or anything on a production-flagged context, requires typing the count or name; `--yes` is for scripts and still honours the safety cap.
  - Reports per-object results and never silently skips failures.
- `ctx` and `ns`: switch cluster context and namespace, like kubectx/kubens, in one tool.
  - `ctx` / `ns` open a fuzzy list; `ctx prod-eks`, `ns payments`, and `ns -` (previous) work directly.
  - **OpenShift projects:** on OpenShift, `ns` lists and switches projects (the `oc projects` / `oc project` equivalent), using the same command.
  - Production-looking contexts are color-flagged, and mutating commands ask for confirmation there.
  - The current context and namespace are always shown in the prompt line of mutating commands so you see where you are before acting.

### 3. Multi-cluster, any distribution

- Uses the standard kubeconfig and client-go auth, so EKS (`aws eks get-token`), AKS (kubelogin), GKE (gke-gcloud-auth-plugin), Rancher (proxied kubeconfig), kubeadm/VM clusters, and OpenShift all work through the same path. The tool never implements its own cloud auth.
- Fan-out: `--context 'prod-*'` or `--all-contexts` runs `status` / `why` across clusters in parallel and merges the result per cluster.
- Distribution-aware hints (managed node-group events, OpenShift SCC denials, Rancher project/namespace quirks) are added as optional, detectable rule packs.

## Principles

1. **Read-only by default.** Anything that changes state (`sh`, debug container, restart) is explicit and clearly labelled.
2. **No agent, no server, no telemetry.** One static binary using your existing kubeconfig.
3. **Least privilege.** Document the minimal RBAC for each command; degrade gracefully (and say so) when a permission is missing.
4. **Never print secret values.** Secret data and env values from Secrets are redacted by default.
5. **Pipe-friendly.** Stable exit codes and `-o json` so it works in scripts and CI.
6. **Rules first, AI optional.**

## About "AI summary"

An optional, **off by default** feature: the already-collected, already-redacted diagnosis (events, status, log tail) is sent to an LLM you configure (a hosted API or a local model such as Ollama) to produce a plain-language explanation and suggested next steps. Because logs and events can contain sensitive data, it must be explicitly enabled, must support local-only models, and must show exactly what would be sent (`--dry-run`). The MVP ships rules only; AI is a later, pluggable layer.

## MVP scope

`status`, `why`, `events`, `sh`, `logs`, `top`, `rm`, `ctx`, and `ns`. Everything else (`changes`, fan-out, rule packs, AI) follows in later minor releases.

## Platforms

Single, self-contained binary with no runtime dependency (no `kubectl`, `oc`, or shell required). Supported targets:

| OS | amd64 | arm64 |
|----|:-----:|:-----:|
| Linux | yes | yes |
| macOS | yes | yes (Apple Silicon) |
| Windows | yes | yes |

Windows is a first-class target: interactive `sh` uses the Windows console API for raw terminal mode, paths and `KUBECONFIG` separators follow OS rules, and colors degrade cleanly. The same CI matrix runs tests on all three operating systems.

## Technology

- Go, client-go, single static binary; distributed as a standalone CLI and as a `kubectl-` plugin via krew.
- See [RELEASING.md](RELEASING.md) for packaging and versioning, and [../SECURITY.md](../SECURITY.md) for the security model.

## Open questions

- Final name and CLI command.
- Rule format: built into the binary, or loadable YAML rule packs from day one?
- How far to go with `changes` without an in-cluster component (Kubernetes only keeps events for about an hour by default).
