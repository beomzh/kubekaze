package cli

import "github.com/spf13/cobra"

const (
	groupTriage = "triage"
	groupDaily  = "daily"
	groupSetup  = "setup"
)

type helpInfo struct {
	group   string
	example string
}

// help holds the grouping and examples shown by --help.
var help = map[string]helpInfo{
	"status": {groupTriage, `  # what is unhealthy in the current namespace?
  kubekaze status

  # the whole cluster, as JSON for a script (exit code 3 = problems found)
  kubekaze status -A -o json`},
	"why": {groupTriage, `  # why is this pod not working?
  kubekaze why api-7d9f

  # the unhealthy pods of a deployment
  kubekaze why deploy/api`},
	"events": {groupTriage, `  # warnings from all namespaces, newest at the bottom
  kubekaze events -A --type Warning

  # which namespace is on fire?
  kubekaze events -A --summary

  # one object, last 30 minutes
  kubekaze events --for pod/api-7d9f --since 30m`},
	"logs": {groupTriage, `  # all pods of a deployment, previous run of crashed containers first
  kubekaze logs deploy/api

  # follow, last 10 minutes, one container
  kubekaze logs -l app=web -c nginx -f --since 10m`},
	"sh": {groupDaily, `  # open a shell; kubekaze finds bash/sh/ash for you
  kubekaze sh api-7d9f

  # pick a pod of a workload, a specific container
  kubekaze sh deploy/api -c sidecar

  # run one command instead of a shell
  kubekaze sh api-7d9f -- ls -l /app`},
	"rm": {groupDaily, `  # preview first, then delete crashing and evicted pods
  kubekaze rm pods --status CrashLoopBackOff,Evicted --dry-run
  kubekaze rm pods --status CrashLoopBackOff,Evicted

  # by name pattern and age, in every namespace
  kubekaze rm pods 'api-*' --older-than 2h -A`},
	"ctx": {groupDaily, `  kubekaze ctx            # pick a context
  kubekaze ctx prod-eks   # switch directly
  kubekaze ctx -          # back to the previous one`},
	"ns": {groupDaily, `  kubekaze ns             # pick a namespace (projects on OpenShift)
  kubekaze ns payments    # switch directly
  kubekaze ns -           # back to the previous one`},
	"completion": {groupSetup, `  # try it in the current shell
  source <(kubekaze completion bash)

  # install for your shell once
  kubekaze completion install`},
	"version": {groupSetup, `  kubekaze version`},
}

const rootExample = `  # start here
  kubekaze status -A          what is unhealthy?
  kubekaze why <pod>          and why?
  kubekaze sh <pod>           get into it
  kubekaze events -A --summary

  # tab completion for your shell
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
	for _, c := range root.Commands() {
		if h, ok := help[c.Name()]; ok {
			c.GroupID = h.group
			if c.Example == "" {
				c.Example = h.example
			}
		}
	}
}
