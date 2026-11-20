package cli

import "github.com/spf13/cobra"

const (
	groupTriage = "triage"
	groupDaily  = "daily"
	groupSetup  = "setup"
)

type helpInfo struct {
	group   string
	long    string
	example string
}

// help holds the grouping, the long description and the examples shown by
// --help. Keeping them here (instead of next to each command) makes it easy to
// review the help text as a whole.
var help = map[string]helpInfo{
	"top": {groupTriage, `Show a usage snapshot from the metrics API (metrics-server): which node is
full, which pods use the most CPU or memory, which namespace is the heaviest.
Read-only.

  Nodes        CPU and memory used vs allocatable, highest first; above 80% is red
  Pods         the top pods (--limit, default 10) by --sort cpu|memory in the
               current namespace, or in every namespace with -A; usage is also
               shown as a percent of the limit, so pods close to throttling or
               OOM stand out ("-" means no limit is set)
  top ns       usage summed per namespace (all namespaces)

With -w the screen is redrawn every --interval (default 5s) when stdout is a
terminal; otherwise each snapshot is printed in turn. Ctrl-C exits with 130.

If metrics-server is not installed or not ready, or you may not read
metrics.k8s.io, a single line says what to install or which RBAC is missing and
the exit code is 1. Use -o json for scripts.`,
		`  # nodes plus the 10 busiest pods in this namespace
  kubekaze top

  # the 20 pods using the most memory in the whole cluster
  kubekaze top -A --sort memory --limit 20

  # which namespace is heaviest?
  kubekaze top ns

  # live view, every 2 seconds
  kubekaze top -A -w --interval 2s

  # for scripts
  kubekaze top -A -o json`},

	"status": {groupTriage, `Show only what is unhealthy, so you do not have to read long lists of healthy objects.

What is checked:
  Nodes        NotReady or Unknown (no heartbeat), memory/disk/PID pressure,
               network unavailable, cordoned (SchedulingDisabled)
  Workloads    Deployments, StatefulSets and DaemonSets with fewer ready replicas
               than wanted (scaled to 0 is fine)
  Pods         CrashLoopBackOff, ImagePullBackOff, OOMKilled, Error, Evicted,
               Pending or Terminating for more than 2 minutes, 5+ restarts
  Jobs         failed Jobs
  Volumes      PersistentVolumeClaims Pending for a while, or Lost

Nodes are cluster-wide, so they are checked in every scope. If you may not list
nodes (common on OpenShift) they are skipped quietly. Anything else you may not
read is listed as "skipped" at the end.

Next step: pass any line to "kubekaze why <kind>/<name>".

Exit codes: 0 healthy, 3 problems found, 1 error. Use -o json for scripts.`,
		`  # what is unhealthy in the current namespace (and in the nodes)?
  kubekaze status

  # the whole cluster
  kubekaze status -A

  # another namespace or cluster, without switching
  kubekaze status -n payments
  kubekaze status --context prod-eks -A

  # use in a script: alert when the exit code is 3
  kubekaze status -A -o json > status.json || echo "problems found"`},

	"ls": {groupDaily, `See what exists in a namespace on one screen, without knowing any resource name.

The overview shows, for the current namespace (-n to pick one, -A for all):
  Deployments, StatefulSets, DaemonSets   ready/desired; unhealthy ones first, in red
  Jobs, CronJobs                          state; failed Jobs are unhealthy
  Services, Ingresses, Routes             Routes (OpenShift) only if the cluster has them
  PersistentVolumeClaims                  phase; anything not Bound is unhealthy
  ConfigMaps, Secrets                     a count and the names

Secrets: only names, type and the number of keys are shown. The values are
never printed, not even in -o json.

A kind with more than 10 objects lists only its unhealthy ones and the count of
the healthy ones. Kinds you may not read are reported as "skipped ... (forbidden)"
at the end; kinds with no objects are listed on a "none:" line.

"kubekaze ls <kind>" lists every object of one kind in detail, optionally only
the named ones. Kinds: deploy, sts, ds, job, cronjob, svc, ingress, route, pvc,
cm, secret (singular, plural and the full names also work). Tab completion
offers kinds and object names.`,
		`  # what is in this namespace?
  kubekaze ls

  # another namespace, or everything
  kubekaze ls -n payments
  kubekaze ls -A

  # one kind in detail, or one object
  kubekaze ls deploy
  kubekaze ls pvc
  kubekaze ls secret db-credentials

  # for scripts
  kubekaze ls -o json`},

	"why": {groupTriage, `Explain why something is not working. kubekaze gathers the evidence you would
collect by hand and ranks suspected causes with rules (no AI, nothing is sent
anywhere).

For a pod:
  state, restart count, last termination reason and exit code, events, the owner
  chain (Pod -> ReplicaSet -> Deployment), whether a rollout started recently,
  and the last log lines of the failing container (its previous run when it
  restarted). Log lines have common secrets masked unless --no-redact is given.

For a workload (deploy/, sts/, ds/, job/): the unhealthy pods of that workload.

For a node (node/<name>): conditions, heartbeat, taints, node events and the
unhealthy pods running on it, with causes such as kubelet down, disk or memory
pressure, CNI or runtime problems.

For all nodes ("kubekaze why node", or "nodes"): every node is checked and only
the ones with problems are printed, plus a one-line summary of the healthy ones.
(A pod that is really called "node" is reached as pod/node.)

Causes it knows: OOMKilled, crash with exit code, image pull failures (not
found, unauthorized, network, rate limit), missing ConfigMap/Secret, init
container failures, scheduling (insufficient resources, taints, affinity,
unbound PVC), failing probes, volume mount errors, eviction, stuck terminating.

Exit codes: 0 no problem found, 3 problem found, 1 error.`,
		`  # a pod (the name prefix is enough to tab-complete)
  kubekaze why api-7d9f-xk2lp

  # the unhealthy pods of a deployment, a statefulset or a job
  kubekaze why deploy/api
  kubekaze why sts/db

  # all nodes: only the ones with problems are shown
  kubekaze why node

  # one node, for example one that shows NotReady in "kubekaze status"
  kubekaze why node/worker-3

  # another namespace; more log lines; machine readable
  kubekaze why api-7d9f -n payments --log-lines 40
  kubekaze why deploy/api -o json`},

	"events": {groupTriage, `Show events the way you actually need them. "kubectl get events" is unsorted,
one namespace at a time and full of repeats; kubekaze fixes that:

  - sorted by last-seen time, newest at the bottom (so the latest problem is
    right above your prompt); change it with --sort and --reverse
  - several namespaces at once: -A, or -n a,b,c
  - repeated events are folded into one line: "(x37 over 12m)"; --no-collapse
    shows them all
  - filters: --type, --reason, --since, --for
  - --summary: one line per namespace with warning counts and the top reasons,
    the quickest way to see which namespace is on fire
  - -w follows new events

Kubernetes keeps events for about an hour by default, so older problems may not
show up here.`,
		`  # warnings from everywhere, newest at the bottom
  kubekaze events -A --type Warning

  # which namespace is on fire?
  kubekaze events -A --summary

  # two namespaces, last 30 minutes
  kubekaze events -n payments,orders --since 30m

  # one object, or one kind of problem
  kubekaze events --for pod/api-7d9f
  kubekaze events -A --reason BackOff,FailedScheduling

  # the most repeated events first
  kubekaze events -A --sort count --reverse

  # follow live
  kubekaze events -A --type Warning -w`},

	"logs": {groupTriage, `Tail the logs of one pod, every pod of a workload, or every pod matching a label
selector (like stern), with crash awareness:

  - when a container restarted, its previous run is printed first (what
    "kubectl logs -p" does), so the reason for the crash is not lost
  - every container is shown unless you pick one with -c
  - lines are prefixed with pod/container and coloured per pod
  - error and warning lines are highlighted
  - common secrets (passwords, tokens, keys, URLs with credentials) are masked;
    --no-redact turns that off

Only pods that exist when the command starts are followed with -f.`,
		`  # one pod
  kubekaze logs api-7d9f

  # every pod of a deployment (previous run of crashed containers comes first)
  kubekaze logs deploy/api

  # follow, only the last 10 minutes
  kubekaze logs deploy/api -f --since 10m

  # by label, one container, last 50 lines each
  kubekaze logs -l app=web -c nginx --tail 50

  # plain output for grep: no prefix, no previous run
  kubekaze logs deploy/api --no-prefix --no-previous | grep ERROR`},

	"sh": {groupDaily, `Open an interactive shell in a pod without guessing which shell the image has.

Target:   a pod name, a workload (deploy/api, sts/db, ds/agent, job/x) or a
          label selector (-l). For a workload a running, ready pod is chosen,
          and you pick one if there are several.
Container: -c, else the pod's default-container annotation, else the only
          container, else you pick.
Shell:    bash, sh, ash and "busybox sh" are tried in that order. Use --shell
          to force one (for example --shell /bin/zsh).

No shell in the image (distroless, scratch)? kubekaze offers an ephemeral debug
container: unprivileged, no extra capabilities, image pinned by digest, sharing
the process namespace with the target container (so you can see its processes).
Choose another image with --debug-image or KUBEKAZE_DEBUG_IMAGE; "latest" and
untagged images are refused. Ephemeral containers stay in the pod until it is
deleted. This needs Kubernetes 1.25+ and the pods/ephemeralcontainers permission.

To run a single command instead of a shell, put it after "--"; no terminal is
needed and the command's exit code becomes kubekaze's exit code.

On a production-looking context you are asked to confirm first (--yes skips it).`,
		`  # a pod; kubekaze finds bash/sh/ash for you
  kubekaze sh api-7d9f

  # a pod of a deployment, a specific container
  kubekaze sh deploy/api
  kubekaze sh deploy/api -c sidecar

  # by label, in another namespace
  kubekaze sh -l app=web -n payments

  # one command instead of a shell (works in scripts)
  kubekaze sh api-7d9f -- ls -l /app
  kubekaze sh deploy/api -- sh -c 'echo $HOSTNAME'

  # an image without a shell: debug container, or choose your own tools image
  kubekaze sh api-7d9f --debug
  kubekaze sh api-7d9f --debug --debug-image nicolaka/netshoot:v0.13`},

	"rm": {groupDaily, `Delete many pods at once. Instead of chaining get | grep | awk | xargs kubectl
delete, say what you want to remove and kubekaze shows exactly that first.

Select pods (all given criteria must match):
  names         pod names or globs: 'api-*' 'web-?'    (quote them)
  -l            label selector
  --status      CrashLoopBackOff, ImagePullBackOff, Error, Evicted, OOMKilled,
                Completed, Pending, Terminating ... (comma separated; any of)
  --older-than  30m, 2h, 1d
  --node        only pods on this node
  -A            in every namespace
With no selection at all, in a terminal, you pick from a list (Tab marks).

Safety, in this order:
  1. a preview of the pods is always shown; --dry-run stops there
  2. you confirm: y/N; for 10 or more pods you type the count; on a
     production-looking context you type the context name
  3. pods are deleted by UID, so a pod recreated under the same name in the
     meantime is not touched
  4. --max (default 50) refuses a run that selects more pods, even with --yes
  5. every pod's result is printed, failures included; the exit code is 1 if any failed

For scripts: --yes skips the question; on a production-looking context also pass
--confirm-context=<context name>. Only pods are supported for now.`,
		`  # look first, then delete (CrashLoopBackOff and Evicted pods)
  kubekaze rm pods --status CrashLoopBackOff,Evicted --dry-run
  kubekaze rm pods --status CrashLoopBackOff,Evicted

  # by name pattern and age
  kubekaze rm pods 'api-*' --older-than 2h

  # leftovers of finished jobs in every namespace
  kubekaze rm pods -A --status Completed --older-than 1d

  # everything on a node that is being drained
  kubekaze rm pods -A --node worker-3 --dry-run

  # pick from a list in the terminal
  kubekaze rm pods

  # in a script (add --confirm-context=<name> on production)
  kubekaze rm pods -n ci --status Error --yes`},

	"ctx": {groupDaily, `List or switch the kubeconfig context (which cluster and user you talk to).

  kubekaze ctx           pick from a list in a terminal, or print the list
  kubekaze ctx NAME      switch (the name can be tab-completed)
  kubekaze ctx -         switch back to the previous context
  kubekaze ctx -c        print the current context

In the picker, contexts that share a cluster and user are shown as one row.
That is what "oc login" / "oc project" produce: one context per namespace,
named <namespace>/<cluster>/<user>. The namespace is a separate choice, made
with "kubekaze ns"; use --all to see every raw context.

After switching, kubekaze checks that the API server is reachable and accepts
your login, and warns you at once if not (for example an expired token: log in
again with oc login, aws eks update-kubeconfig, ...). --no-check skips this.

Contexts whose name looks like production are marked [prod] and trigger extra
confirmations in sh and rm. Change the pattern with KUBEKAZE_PROD_PATTERN.

Note: like kubectx, this edits the kubeconfig file (current-context only), so
other terminals that use the same file are switched as well.`,
		`  # pick one
  kubekaze ctx

  # switch directly, back and forth
  kubekaze ctx prod-eks
  kubekaze ctx -

  # every context, not grouped (OpenShift)
  kubekaze ctx --all

  # where am I?
  kubekaze ctx -c

  # just run one command elsewhere without switching
  kubekaze status --context staging -A`},

	"ns": {groupDaily, `List or switch the namespace of the current context. On OpenShift it lists and
switches projects (what "oc projects" and "oc project" do).

  kubekaze ns            pick from a list in a terminal, or print the list
  kubekaze ns NAME       switch (the name can be tab-completed)
  kubekaze ns -          switch back to the previous namespace
  kubekaze ns -c         print the current namespace
  kubekaze ns --list     list with pods, unhealthy pods and recent Warning events
                         (-o json for scripts; plain "ns" piped prints names only)

The picker shows the same summary next to each name: pod count, unhealthy pods
(same rules as "status") and Warning events from the last hour. Filtering
matches the name. Pods and events are read once cluster-wide; if you are not
allowed to, a note says so and the plain list is shown. --no-summary skips them.

Only the namespace field of the current context is changed in your kubeconfig,
and the previous value is remembered per context for "ns -". If the namespace
does not exist on the cluster you get a warning, because you may simply lack
the permission to look.

To use another namespace for a single command, pass -n instead: it never
changes your kubeconfig.`,
		`  # pick one (projects on OpenShift)
  kubekaze ns

  # switch directly, back and forth
  kubekaze ns payments
  kubekaze ns -

  # where am I?
  kubekaze ns -c

  # what is going on in each namespace? (also for pipes and scripts)
  kubekaze ns --list
  kubekaze ns -o json

  # one command in another namespace, without switching
  kubekaze logs deploy/api -n payments`},

	"completion": {groupSetup, "", `  # try it in the current shell
  source <(kubekaze completion bash)

  # install for your shell once
  kubekaze completion install

  # you use alias kk=kubekaze
  kubekaze completion install --alias kk`},
	"update": {groupSetup, `Update kubekaze to the latest release. kubekaze never checks for updates on its
own; it only contacts GitHub (or your mirror) when you run this command.

Online:
  kubekaze update --check     is there a newer version?
  kubekaze update             download it, verify its checksum, replace this binary

No internet (air-gapped network): download the archive for your OS and CPU and
checksums.txt from https://github.com/beomzh/kubekaze/releases on a machine that
has access, copy both files to the server, then:
  kubekaze update --from kubekaze_0.1.1_linux_amd64.tar.gz
The checksums.txt next to the archive is used (or pass --checksums). Nothing is
downloaded in this mode. uname -m tells you the CPU: x86_64 is amd64, aarch64 is arm64.

Internal mirror (Nexus, Artifactory, a web server): publish the release files as
  <mirror>/latest                   one line with the newest tag, e.g. v0.1.1
  <mirror>/v0.1.1/checksums.txt
  <mirror>/v0.1.1/kubekaze_0.1.1_linux_amd64.tar.gz   (and the other platforms)
and run: kubekaze update --url https://mirror.example.com/kubekaze
(or set KUBEKAZE_UPDATE_URL). Without a latest file, name the version: --version v0.1.1.
Only https is accepted, except for localhost.

How it stays safe:
  - the archive must match the SHA-256 in checksums.txt, or nothing is changed
  - the new binary is started once (--version) before it replaces the old one
  - the replacement is a single atomic rename; on any failure the old binary stays
  - installs owned by Homebrew or krew are left alone (use brew/krew to upgrade)
The checksum proves the file arrived intact. It does not prove who built it: for
that, verify the cosign signature of checksums.txt (see SECURITY.md) before
carrying files into a restricted network.

Permissions: if the binary lives in a directory you cannot write (for example
/usr/local/bin or /bin), run it with sudo: sudo $(which kubekaze) update
(on Windows a running .exe cannot be overwritten, so the old one is renamed to
kubekaze.exe.old and removed by the next update).`,
		`  # is there something new?
  kubekaze update --check

  # update (asks first; -y to skip the question)
  kubekaze update
  sudo $(which kubekaze) update

  # a specific version, or go back to one
  kubekaze update --version v0.1.1
  kubekaze update --version v0.1.0 --force

  # air-gapped: the files were copied to this server
  kubekaze update --from ./kubekaze_0.1.1_linux_amd64.tar.gz

  # internal mirror
  kubekaze update --url https://mirror.example.com/kubekaze`},
	"version": {groupSetup, "", `  kubekaze version`},
}

const rootLong = `kubekaze finds out what is wrong in your cluster and why, in one command.

It uses your existing kubeconfig (the same one kubectl and oc use) and is
read-only unless a command says otherwise: sh, rm, ctx and ns change things.
Works with any Kubernetes: kubeadm/VM, EKS, AKS, GKE, OpenShift, Rancher.

Where to start:
  1. kubekaze status -A       what is unhealthy?
  2. kubekaze why <name>      why? (a pod, deploy/x, node/x, or just: node)
  3. kubekaze logs / sh       look inside, fix, then
  4. kubekaze rm pods ...     clean up

Naming things: most commands take a pod name, a workload like deploy/api,
sts/db, ds/agent, job/x, or a label selector with -l.

Exit codes:  0 ok   1 error   2 wrong command, flag or argument
             3 problems found (status, why)   130 cancelled
Environment: KUBECONFIG, KUBEKAZE_PROD_PATTERN (regex for production-looking
             context names), KUBEKAZE_DEBUG_IMAGE, NO_COLOR

Run 'kubekaze <command> --help' for details and examples.`

const rootExample = `  # start here
  kubekaze status -A
  kubekaze why <pod>
  kubekaze events -A --summary

  # get in, look around
  kubekaze logs deploy/api
  kubekaze sh deploy/api

  # switch cluster / namespace
  kubekaze ctx
  kubekaze ns

  # tab completion (if you already use alias kk=kubekaze, add: --alias kk)
  kubekaze completion install`

// applyHelp sets groups and examples on the registered commands.
func applyHelp(root *cobra.Command) {
	root.AddGroup(
		&cobra.Group{ID: groupTriage, Title: "Troubleshooting:"},
		&cobra.Group{ID: groupDaily, Title: "Daily use:"},
		&cobra.Group{ID: groupSetup, Title: "Setup:"},
	)
	root.SetHelpCommandGroupID(groupSetup)
	root.Example = rootExample
	root.Long = rootLong
	for _, c := range root.Commands() {
		if h, ok := help[c.Name()]; ok {
			c.GroupID = h.group
			if h.long != "" {
				c.Long = h.long
			}
			if h.example != "" {
				c.Example = h.example
			}
		}
	}
}
