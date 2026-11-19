package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/beomzh/kubekaze/internal/term"
)

// lsHealthyCap: a section with more objects than this is summarised (only the
// unhealthy ones are listed) in the overview.
const lsHealthyCap = 10

// lsNameCap bounds the names printed on one line for ConfigMaps and Secrets.
const lsNameCap = 8

// lsItem is one object in a section. Cols follow the section header.
type lsItem struct {
	Namespace string            `json:"namespace,omitempty"`
	Name      string            `json:"name"`
	Healthy   bool              `json:"healthy"`
	Status    string            `json:"status,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
	cols      []string
}

type lsSection struct {
	Key    string   `json:"kind"`
	Title  string   `json:"-"`
	Header []string `json:"-"`
	Items  []lsItem `json:"items"`
	// namesOnly sections (ConfigMaps, Secrets) are summarised as a name list.
	namesOnly bool
}

type lsKind struct {
	key     string
	title   string
	aliases []string
	names   bool
	header  []string
	fetch   func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error)
}

func newItem(ns, name string, healthy bool, status string, header []string, cols ...string) lsItem {
	it := lsItem{Namespace: ns, Name: name, Healthy: healthy, Status: status, cols: cols, Details: map[string]string{}}
	for i, h := range header {
		if i < len(cols) {
			it.Details[strings.ToLower(strings.ReplaceAll(h, " ", "_"))] = cols[i]
		}
	}
	return it
}

func lsAge(t metav1.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t.Time)
	switch {
	case d < 2*time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < 2*time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

func replicas(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}

var lsKinds = []lsKind{
	{key: "deployments", title: "Deployments", aliases: []string{"deploy", "deployment", "deployments"},
		header: []string{"READY", "UP-TO-DATE", "AVAILABLE", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"READY", "UP-TO-DATE", "AVAILABLE", "AGE"}
			var out []lsItem
			for _, d := range l.Items {
				want := replicas(d.Spec.Replicas)
				ok := want == 0 || d.Status.ReadyReplicas >= want
				out = append(out, newItem(d.Namespace, d.Name, ok, readyStatus(ok, d.Status.ReadyReplicas, want), h,
					fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, want), strconv.Itoa(int(d.Status.UpdatedReplicas)), strconv.Itoa(int(d.Status.AvailableReplicas)), lsAge(d.CreationTimestamp)))
			}
			return out, nil
		}},
	{key: "statefulsets", title: "StatefulSets", aliases: []string{"sts", "statefulset", "statefulsets"},
		header: []string{"READY", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"READY", "AGE"}
			var out []lsItem
			for _, s := range l.Items {
				want := replicas(s.Spec.Replicas)
				ok := want == 0 || s.Status.ReadyReplicas >= want
				out = append(out, newItem(s.Namespace, s.Name, ok, readyStatus(ok, s.Status.ReadyReplicas, want), h,
					fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, want), lsAge(s.CreationTimestamp)))
			}
			return out, nil
		}},
	{key: "daemonsets", title: "DaemonSets", aliases: []string{"ds", "daemonset", "daemonsets"},
		header: []string{"READY", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"READY", "AGE"}
			var out []lsItem
			for _, d := range l.Items {
				want := d.Status.DesiredNumberScheduled
				ok := want == 0 || d.Status.NumberReady >= want
				out = append(out, newItem(d.Namespace, d.Name, ok, readyStatus(ok, d.Status.NumberReady, want), h,
					fmt.Sprintf("%d/%d", d.Status.NumberReady, want), lsAge(d.CreationTimestamp)))
			}
			return out, nil
		}},
	{key: "jobs", title: "Jobs", aliases: []string{"job", "jobs"},
		header: []string{"STATUS", "COMPLETIONS", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"STATUS", "COMPLETIONS", "AGE"}
			var out []lsItem
			for _, j := range l.Items {
				st := jobState(j)
				want := "1"
				if j.Spec.Completions != nil {
					want = strconv.Itoa(int(*j.Spec.Completions))
				}
				out = append(out, newItem(j.Namespace, j.Name, st != "Failed", st, h,
					st, fmt.Sprintf("%d/%s", j.Status.Succeeded, want), lsAge(j.CreationTimestamp)))
			}
			return out, nil
		}},
	{key: "cronjobs", title: "CronJobs", aliases: []string{"cronjob", "cronjobs", "cj"},
		header: []string{"SCHEDULE", "SUSPEND", "ACTIVE", "LAST SCHEDULE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"SCHEDULE", "SUSPEND", "ACTIVE", "LAST SCHEDULE"}
			var out []lsItem
			for _, c := range l.Items {
				susp := c.Spec.Suspend != nil && *c.Spec.Suspend
				last := "-"
				if c.Status.LastScheduleTime != nil {
					last = lsAge(*c.Status.LastScheduleTime) + " ago"
				}
				out = append(out, newItem(c.Namespace, c.Name, true, "", h,
					c.Spec.Schedule, strconv.FormatBool(susp), strconv.Itoa(len(c.Status.Active)), last))
			}
			return out, nil
		}},
	{key: "services", title: "Services", aliases: []string{"svc", "service", "services"},
		header: []string{"TYPE", "CLUSTER-IP", "PORTS", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"TYPE", "CLUSTER-IP", "PORTS", "AGE"}
			var out []lsItem
			for _, s := range l.Items {
				var ports []string
				for _, p := range s.Spec.Ports {
					ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
				}
				out = append(out, newItem(s.Namespace, s.Name, true, "", h,
					string(s.Spec.Type), s.Spec.ClusterIP, strings.Join(ports, ","), lsAge(s.CreationTimestamp)))
			}
			return out, nil
		}},
	{key: "ingresses", title: "Ingresses", aliases: []string{"ingress", "ingresses", "ing"},
		header: []string{"CLASS", "HOSTS", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"CLASS", "HOSTS", "AGE"}
			var out []lsItem
			for _, i := range l.Items {
				class := "-"
				if i.Spec.IngressClassName != nil {
					class = *i.Spec.IngressClassName
				}
				var hosts []string
				for _, r := range i.Spec.Rules {
					if r.Host != "" {
						hosts = append(hosts, r.Host)
					}
				}
				if len(hosts) == 0 {
					hosts = []string{"*"}
				}
				out = append(out, newItem(i.Namespace, i.Name, true, "", h, class, strings.Join(hosts, ","), lsAge(i.CreationTimestamp)))
			}
			return out, nil
		}},
	{key: "routes", title: "Routes", aliases: []string{"route", "routes", "rt"},
		header: []string{"HOST", "SERVICE", "ADMITTED", "AGE"},
		fetch:  fetchRoutes},
	{key: "pvcs", title: "PersistentVolumeClaims", aliases: []string{"pvc", "pvcs", "persistentvolumeclaim", "persistentvolumeclaims"},
		header: []string{"PHASE", "CAPACITY", "STORAGECLASS", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"PHASE", "CAPACITY", "STORAGECLASS", "AGE"}
			var out []lsItem
			for _, p := range l.Items {
				capacity := "-"
				if q, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
					capacity = q.String()
				}
				sc := "-"
				if p.Spec.StorageClassName != nil && *p.Spec.StorageClassName != "" {
					sc = *p.Spec.StorageClassName
				}
				out = append(out, newItem(p.Namespace, p.Name, p.Status.Phase == corev1.ClaimBound, string(p.Status.Phase), h,
					string(p.Status.Phase), capacity, sc, lsAge(p.CreationTimestamp)))
			}
			return out, nil
		}},
	{key: "configmaps", title: "ConfigMaps", aliases: []string{"cm", "configmap", "configmaps"}, names: true,
		header: []string{"KEYS", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"KEYS", "AGE"}
			var out []lsItem
			for _, c := range l.Items {
				out = append(out, newItem(c.Namespace, c.Name, true, "", h,
					strconv.Itoa(len(c.Data)+len(c.BinaryData)), lsAge(c.CreationTimestamp)))
			}
			return out, nil
		}},
	// Secrets: only metadata is ever read into an lsItem (name, type, number of
	// keys, age). The data and stringData are never copied or printed.
	{key: "secrets", title: "Secrets", aliases: []string{"secret", "secrets"}, names: true,
		header: []string{"TYPE", "KEYS", "AGE"},
		fetch: func(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
			l, err := cs.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			h := []string{"TYPE", "KEYS", "AGE"}
			var out []lsItem
			for _, s := range l.Items {
				out = append(out, newItem(s.Namespace, s.Name, true, "", h,
					string(s.Type), strconv.Itoa(len(s.Data)), lsAge(s.CreationTimestamp)))
			}
			return out, nil
		}},
}

// overviewOrder is the order of the sections in the overview.
var overviewOrder = []string{"deployments", "statefulsets", "daemonsets", "jobs", "cronjobs", "services", "ingresses", "routes", "pvcs", "configmaps", "secrets"}

func readyStatus(ok bool, ready, want int32) string {
	if ok {
		return ""
	}
	return fmt.Sprintf("%d/%d ready", ready, want)
}

func jobState(j batchv1.Job) string {
	for _, c := range j.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobFailed:
			return "Failed"
		case batchv1.JobComplete:
			return "Complete"
		case batchv1.JobSuspended:
			return "Suspended"
		}
	}
	if j.Status.Active > 0 {
		return "Running"
	}
	return "Pending"
}

func kindByAlias(s string) *lsKind {
	s = strings.ToLower(s)
	for i := range lsKinds {
		for _, al := range lsKinds[i].aliases {
			if al == s {
				return &lsKinds[i]
			}
		}
	}
	return nil
}

func kindByKey(k string) *lsKind {
	for i := range lsKinds {
		if lsKinds[i].key == k {
			return &lsKinds[i]
		}
	}
	return nil
}

// routeList is the subset of the OpenShift Route list we read.
type routeList struct {
	Items []struct {
		Metadata struct {
			Name              string      `json:"name"`
			Namespace         string      `json:"namespace"`
			CreationTimestamp metav1.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Spec struct {
			Host string `json:"host"`
			To   struct {
				Name string `json:"name"`
			} `json:"to"`
		} `json:"spec"`
		Status struct {
			Ingress []struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"ingress"`
		} `json:"status"`
	} `json:"items"`
}

func parseRoutes(raw []byte) ([]lsItem, error) {
	var rl routeList
	if err := json.Unmarshal(raw, &rl); err != nil {
		return nil, err
	}
	h := []string{"HOST", "SERVICE", "ADMITTED", "AGE"}
	var out []lsItem
	for _, r := range rl.Items {
		admitted := "Unknown"
		for _, ing := range r.Status.Ingress {
			for _, c := range ing.Conditions {
				if c.Type == "Admitted" {
					admitted = c.Status
				}
			}
		}
		ok := admitted != "False"
		st := ""
		if !ok {
			st = "not admitted"
		}
		out = append(out, newItem(r.Metadata.Namespace, r.Metadata.Name, ok, st, h,
			r.Spec.Host, r.Spec.To.Name, admitted, lsAge(r.Metadata.CreationTimestamp)))
	}
	return out, nil
}

var errAPIAbsent = fmt.Errorf("API not available")

// hasRouteAPI reports whether the cluster serves route.openshift.io.
func hasRouteAPI(cs kubernetes.Interface) bool {
	groups, err := cs.Discovery().ServerGroups()
	if err != nil || groups == nil {
		return false
	}
	for _, g := range groups.Groups {
		if g.Name == "route.openshift.io" {
			return true
		}
	}
	return false
}

func fetchRoutes(ctx context.Context, cs kubernetes.Interface, ns string) ([]lsItem, error) {
	if !hasRouteAPI(cs) {
		return nil, errAPIAbsent
	}
	rc := cs.Discovery().RESTClient()
	if rc == nil {
		return nil, errAPIAbsent
	}
	path := "/apis/route.openshift.io/v1/routes"
	if ns != "" {
		path = "/apis/route.openshift.io/v1/namespaces/" + ns + "/routes"
	}
	raw, err := rc.Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	return parseRoutes(raw)
}

// lsResult is the full overview, also the -o json document.
type lsResult struct {
	Scope    string      `json:"scope"`
	Sections []lsSection `json:"sections"`
	Skipped  []string    `json:"skipped"`
}

func sortItems(items []lsItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Healthy != b.Healthy {
			return !a.Healthy
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
}

// gather lists the given kinds. Kinds that cannot be read are reported in
// skipped, never dropped silently. A missing optional API (Route on plain
// Kubernetes) is simply not a section.
func gather(ctx context.Context, cs kubernetes.Interface, ns string, keys []string) ([]lsSection, []string) {
	var secs []lsSection
	var skipped []string
	for _, k := range keys {
		kd := kindByKey(k)
		items, err := kd.fetch(ctx, cs, ns)
		if err != nil {
			switch {
			case err == errAPIAbsent:
			case apierrors.IsForbidden(err):
				skipped = append(skipped, kd.title+" (forbidden)")
			case apierrors.IsNotFound(err):
				skipped = append(skipped, kd.title+" (API not available)")
			default:
				skipped = append(skipped, fmt.Sprintf("%s (%v)", kd.title, err))
			}
			continue
		}
		sortItems(items)
		secs = append(secs, lsSection{Key: kd.key, Title: kd.title, Header: kd.header, Items: items, namesOnly: kd.names})
	}
	return secs, skipped
}

func (a *app) newLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls [kind [name...]]",
		Short: "One-screen overview of what exists in a namespace",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runLs(cmd.Context(), args) },
	}
}

func (a *app) runLs(ctx context.Context, args []string) error {
	var kd *lsKind
	if len(args) > 0 {
		if kd = kindByAlias(args[0]); kd == nil {
			return fmt.Errorf("unknown kind %q (try: deploy, sts, ds, job, cronjob, svc, ingress, route, pvc, cm, secret)", args[0])
		}
	}
	cs, err := a.clientset()
	if err != nil {
		return err
	}
	ns, err := a.opts.NamespaceOrAll()
	if err != nil {
		return err
	}
	scope := ns
	if a.opts.AllNamespaces {
		scope = "all namespaces"
	}
	keys := overviewOrder
	if kd != nil {
		keys = []string{kd.key}
	}
	secs, skipped := gather(ctx, cs, ns, keys)
	if kd != nil {
		if len(secs) == 0 {
			if len(skipped) > 0 {
				return fmt.Errorf("cannot list %s", skipped[0])
			}
			return fmt.Errorf("%s: the API is not available on this cluster", kd.title)
		}
		if len(args) > 1 {
			secs[0].Items = filterNames(secs[0].Items, args[1:])
			if len(secs[0].Items) == 0 {
				return fmt.Errorf("no %s named %s in %s", strings.ToLower(kd.title), strings.Join(args[1:], ", "), scope)
			}
		}
	}
	res := lsResult{Scope: scope, Sections: secs, Skipped: skipped}
	if res.Sections == nil {
		res.Sections = []lsSection{}
	}
	if res.Skipped == nil {
		res.Skipped = []string{}
	}
	if a.output == "json" {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	if kd != nil {
		a.printLsDetail(secs[0], a.opts.AllNamespaces)
	} else {
		a.printLsOverview(res, a.opts.AllNamespaces)
	}
	for _, s := range skipped {
		fmt.Fprintln(a.err, a.pal.Dim("skipped "+term.SafeText(s)))
	}
	return nil
}

func filterNames(items []lsItem, names []string) []lsItem {
	var out []lsItem
	for _, it := range items {
		for _, n := range names {
			if it.Name == n {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

func lsTable(a *app, s lsSection, items []lsItem, all bool) {
	header := append([]string{"NAME"}, s.Header...)
	if all {
		header = append([]string{"NAMESPACE"}, header...)
	}
	rows := make([][]cell, 0, len(items))
	for _, it := range items {
		var row []cell
		name := plain(it.Name)
		if !it.Healthy {
			name.style = a.pal.Red
		}
		if all {
			row = append(row, plain(it.Namespace))
		}
		row = append(row, name)
		for i, c := range it.cols {
			cc := plain(c)
			if !it.Healthy && i == 0 {
				cc.style = a.pal.Red
			}
			row = append(row, cc)
		}
		rows = append(rows, row)
	}
	table(a.out, header, rows)
}

func (a *app) printLsDetail(s lsSection, all bool) {
	bad := 0
	for _, it := range s.Items {
		if !it.Healthy {
			bad++
		}
	}
	fmt.Fprintln(a.out, a.pal.Bold(fmt.Sprintf("%s (%d%s)", s.Title, len(s.Items), unhealthyNote(bad))))
	lsTable(a, s, s.Items, all)
}

func unhealthyNote(bad int) string {
	if bad == 0 {
		return ""
	}
	return fmt.Sprintf(", %d unhealthy", bad)
}

func (a *app) printLsOverview(res lsResult, all bool) {
	fmt.Fprintln(a.out, a.pal.Bold("Overview of "+term.SafeText(res.Scope)))
	have := map[string]bool{}
	var empty []string
	unhealthyTotal := 0
	for _, s := range res.Sections {
		have[s.Key] = true
		if len(s.Items) == 0 {
			empty = append(empty, strings.ToLower(s.Title))
			continue
		}
		bad := 0
		for _, it := range s.Items {
			if !it.Healthy {
				bad++
			}
		}
		unhealthyTotal += bad
		fmt.Fprintln(a.out)
		head := fmt.Sprintf("%s (%d%s)", s.Title, len(s.Items), unhealthyNote(bad))
		if bad > 0 {
			fmt.Fprintln(a.out, a.pal.Red(head))
		} else {
			fmt.Fprintln(a.out, a.pal.Bold(head))
		}
		switch {
		case s.namesOnly:
			fmt.Fprintln(a.out, "  "+nameList(s.Items, all))
		case len(s.Items) <= lsHealthyCap:
			lsTable(a, s, s.Items, all)
		default:
			if bad > 0 {
				lsTable(a, s, s.Items[:bad], all)
			}
			fmt.Fprintf(a.out, "%s\n", a.pal.Dim(fmt.Sprintf("  %d healthy, not listed (kubekaze ls %s)", len(s.Items)-bad, kindByKey(s.Key).aliases[0])))
		}
	}
	if len(empty) > 0 {
		fmt.Fprintln(a.out)
		fmt.Fprintln(a.out, a.pal.Dim("none: "+strings.Join(empty, ", ")))
	}
	fmt.Fprintln(a.out)
	if unhealthyTotal > 0 {
		fmt.Fprintln(a.out, a.pal.Yellow(fmt.Sprintf("%d unhealthy: kubekaze status, then kubekaze why <name>", unhealthyTotal)))
	} else {
		fmt.Fprintln(a.out, "Details of one kind: kubekaze ls <kind> (deploy, sts, ds, job, cronjob, svc, ingress, route, pvc, cm, secret)")
	}
}

// nameList prints up to lsNameCap names ("a, b, c, +9 more"). Names only.
func nameList(items []lsItem, all bool) string {
	var parts []string
	for i, it := range items {
		if i == lsNameCap {
			parts = append(parts, fmt.Sprintf("+%d more", len(items)-lsNameCap))
			break
		}
		n := term.SafeText(it.Name)
		if all {
			n = term.SafeText(it.Namespace) + "/" + n
		}
		parts = append(parts, n)
	}
	return strings.Join(parts, ", ")
}

// completeLsArgs completes the kind, then the object names of that kind.
func (a *app) completeLsArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		kinds := []string{"deploy", "sts", "ds", "job", "cronjob", "svc", "ingress", "route", "pvc", "cm", "secret"}
		return filterPrefix(kinds, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	kd := kindByAlias(args[0])
	if kd == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx, cancel := complCtx(cmd)
	defer cancel()
	cs, err := a.clientset()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ns, err := a.opts.ResolvedNamespace()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	items, err := kd.fetch(ctx, cs, ns)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	sort.Strings(names)
	return filterPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}
