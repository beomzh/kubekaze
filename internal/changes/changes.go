// Package changes answers "what changed just before it broke?" from what the
// Kubernetes API remembers: object creation times, field manager timestamps,
// rollout revisions, Helm release records and events. It cannot see deleted
// objects or old values.
package changes

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/events"
	"github.com/beomzh/kubekaze/internal/why"
)

// Change is one thing that happened.
type Change struct {
	Time      time.Time `json:"time"`
	Namespace string    `json:"namespace,omitempty"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Action    string    `json:"action"` // created, rollout, updated, scaled, node
	Detail    string    `json:"detail,omitempty"`
	By        string    `json:"by,omitempty"` // field manager that made the change
}

// Input is what Find looks at. Missing kinds are simply not reported.
type Input struct {
	Deployments  []appsv1.Deployment
	ReplicaSets  []appsv1.ReplicaSet
	StatefulSets []appsv1.StatefulSet
	DaemonSets   []appsv1.DaemonSet
	Revisions    []appsv1.ControllerRevision
	ConfigMaps   []corev1.ConfigMap
	Secrets      []corev1.Secret
	Services     []corev1.Service
	Ingresses    []networkingv1.Ingress
	Events       []events.Row // namespaced events
	NodeEvents   []events.Row
	Since        time.Time
}

const (
	revisionAnnotation = "deployment.kubernetes.io/revision"
	leaderAnnotation   = "control-plane.alpha.kubernetes.io/leader"
	helmSecretType     = "helm.sh/release.v1"
	// near is how close two records must be to describe the same change.
	near = 2 * time.Minute
)

// Find returns the changes at or after in.Since, oldest first.
func Find(in Input) []Change {
	f := finder{since: in.Since}
	f.deployments(in.Deployments, in.ReplicaSets)
	for i := range in.StatefulSets {
		o := &in.StatefulSets[i]
		f.workload("StatefulSet", &o.ObjectMeta, o.Spec.Replicas, in.Revisions)
	}
	for i := range in.DaemonSets {
		o := &in.DaemonSets[i]
		f.workload("DaemonSet", &o.ObjectMeta, nil, in.Revisions)
	}
	for i := range in.ConfigMaps {
		f.configMap(&in.ConfigMaps[i])
	}
	for i := range in.Secrets {
		f.secret(&in.Secrets[i])
	}
	f.helm(in.Secrets)
	for i := range in.Services {
		f.object("Service", &in.Services[i].ObjectMeta, "spec changed", "f:spec")
	}
	for i := range in.Ingresses {
		f.object("Ingress", &in.Ingresses[i].ObjectMeta, "spec changed", "f:spec")
	}
	f.events(in.Events)
	f.nodes(in.NodeEvents)
	sort.SliceStable(f.out, func(i, j int) bool { return f.out[i].Time.Before(f.out[j].Time) })
	return f.out
}

type finder struct {
	since time.Time
	out   []Change
}

func (f *finder) recent(t time.Time) bool { return !t.IsZero() && !t.Before(f.since) }

func (f *finder) add(c Change) { f.out = append(f.out, c) }

// lastEdit returns the newest field manager entry that touched one of the
// fields. Status writes are not changes anyone made to the object.
func lastEdit(m *metav1.ObjectMeta, fields ...string) (time.Time, string, bool) {
	var at time.Time
	by, scale := "", false
	for _, e := range m.ManagedFields {
		if e.Time == nil || e.Subresource == "status" || e.FieldsV1 == nil {
			continue
		}
		raw := string(e.FieldsV1.Raw)
		for _, field := range fields {
			if strings.Contains(raw, `"`+field+`"`) && e.Time.After(at) {
				at, by, scale = e.Time.Time, e.Manager, e.Subresource == "scale"
			}
		}
	}
	return at, by, scale
}

// creator is the manager of the oldest entry, normally who created the object.
func creator(m *metav1.ObjectMeta) string {
	var at time.Time
	by := ""
	for _, e := range m.ManagedFields {
		if e.Time != nil && e.Subresource == "" && (at.IsZero() || e.Time.Time.Before(at)) {
			at, by = e.Time.Time, e.Manager
		}
	}
	return by
}

// object reports creation, or else a later edit of one of the fields.
func (f *finder) object(kind string, m *metav1.ObjectMeta, detail string, fields ...string) {
	c := Change{Namespace: m.Namespace, Kind: kind, Name: m.Name}
	if f.recent(m.CreationTimestamp.Time) {
		c.Time, c.Action, c.By = m.CreationTimestamp.Time, "created", creator(m)
		f.add(c)
		return
	}
	if at, by, _ := lastEdit(m, fields...); f.recent(at) {
		c.Time, c.Action, c.Detail, c.By = at, "updated", detail, by
		f.add(c)
	}
}

func (f *finder) configMap(o *corev1.ConfigMap) {
	// written by the cluster itself all the time: not a change anyone made
	if o.Name == "kube-root-ca.crt" || o.Name == "openshift-service-ca.crt" || o.Annotations[leaderAnnotation] != "" {
		return
	}
	f.object("ConfigMap", &o.ObjectMeta, "data changed", "f:data", "f:binaryData")
}

// secret never looks at secret values.
func (f *finder) secret(o *corev1.Secret) {
	switch o.Type {
	case corev1.SecretTypeServiceAccountToken, corev1.SecretTypeDockercfg:
		return
	case helmSecretType:
		return // reported per release by helm
	}
	f.object("Secret", &o.ObjectMeta, "data changed", "f:data", "f:stringData")
}

// helm reports one line per release, however many revisions it got.
func (f *finder) helm(secrets []corev1.Secret) {
	type release struct {
		latest      *corev1.Secret
		first, last int
	}
	byName := map[string]*release{}
	var keys []string
	for i := range secrets {
		s := &secrets[i]
		if s.Type != helmSecretType || !f.recent(s.CreationTimestamp.Time) {
			continue
		}
		v, _ := strconv.Atoi(s.Labels["version"])
		key := s.Namespace + "/" + s.Labels["name"]
		r := byName[key]
		if r == nil {
			r = &release{latest: s, first: v, last: v}
			byName[key] = r
			keys = append(keys, key)
			continue
		}
		r.first = min(r.first, v)
		if v > r.last {
			r.latest, r.last = s, v
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		r := byName[key]
		detail := fmt.Sprintf("revision %d", r.last)
		if r.first != r.last {
			detail = fmt.Sprintf("revisions %d-%d", r.first, r.last)
		}
		f.add(Change{Time: r.latest.CreationTimestamp.Time, Namespace: r.latest.Namespace, Kind: "HelmRelease",
			Name: r.latest.Labels["name"], Action: "rollout", Detail: detail + " (" + r.latest.Labels["status"] + ")", By: "helm"})
	}
}

func owner(m *metav1.ObjectMeta, kind string) string {
	for _, o := range m.OwnerReferences {
		if o.Kind == kind {
			return o.Name
		}
	}
	return ""
}

func (f *finder) deployments(deps []appsv1.Deployment, sets []appsv1.ReplicaSet) {
	byOwner := map[string][]*appsv1.ReplicaSet{}
	for i := range sets {
		rs := &sets[i]
		if o := owner(&rs.ObjectMeta, "Deployment"); o != "" {
			byOwner[rs.Namespace+"/"+o] = append(byOwner[rs.Namespace+"/"+o], rs)
		}
	}
	for i := range deps {
		d := &deps[i]
		m := &d.ObjectMeta
		if f.recent(m.CreationTimestamp.Time) {
			f.add(Change{Time: m.CreationTimestamp.Time, Namespace: m.Namespace, Kind: "Deployment", Name: m.Name,
				Action: "created", Detail: images(d.Spec.Template.Spec), By: creator(m)})
			continue
		}
		editAt, by, scaled := lastEdit(m, "f:spec")
		rolled := false
		list := byOwner[m.Namespace+"/"+m.Name]
		sort.Slice(list, func(i, j int) bool { return revision(list[i]) < revision(list[j]) })
		for j, rs := range list {
			at := rs.CreationTimestamp.Time
			if !f.recent(at) {
				continue
			}
			detail := fmt.Sprintf("revision %d", revision(rs))
			if j > 0 {
				detail += ": " + templateDiff(list[j-1].Spec.Template.Spec, rs.Spec.Template.Spec)
			}
			c := Change{Time: at, Namespace: m.Namespace, Kind: "Deployment", Name: m.Name, Action: "rollout", Detail: detail}
			if f.recent(editAt) && editAt.Sub(at).Abs() <= near {
				c.By, rolled = by, true
			}
			f.add(c)
		}
		// an edit that did not start a rollout: replicas, strategy, a rollback...
		if !rolled && f.recent(editAt) && by != "kube-controller-manager" {
			f.add(edit("Deployment", m, d.Spec.Replicas, editAt, by, scaled))
		}
	}
}

func edit(kind string, m *metav1.ObjectMeta, replicas *int32, at time.Time, by string, scaled bool) Change {
	c := Change{Time: at, Namespace: m.Namespace, Kind: kind, Name: m.Name, Action: "updated", Detail: "spec changed", By: by}
	if scaled && replicas != nil {
		c.Action, c.Detail = "scaled", "now "+strconv.Itoa(int(*replicas))+" replica(s)"
	}
	return c
}

// workload handles StatefulSets and DaemonSets, whose history is kept in
// ControllerRevisions.
func (f *finder) workload(kind string, m *metav1.ObjectMeta, replicas *int32, revs []appsv1.ControllerRevision) {
	if f.recent(m.CreationTimestamp.Time) {
		f.add(Change{Time: m.CreationTimestamp.Time, Namespace: m.Namespace, Kind: kind, Name: m.Name, Action: "created", By: creator(m)})
		return
	}
	editAt, by, scaled := lastEdit(m, "f:spec")
	rolled := false
	for i := range revs {
		r := &revs[i]
		at := r.CreationTimestamp.Time
		if r.Namespace != m.Namespace || owner(&r.ObjectMeta, kind) != m.Name || !f.recent(at) {
			continue
		}
		c := Change{Time: at, Namespace: m.Namespace, Kind: kind, Name: m.Name, Action: "rollout", Detail: fmt.Sprintf("revision %d", r.Revision)}
		if f.recent(editAt) && editAt.Sub(at).Abs() <= near {
			c.By, rolled = by, true
		}
		f.add(c)
	}
	if !rolled && f.recent(editAt) {
		f.add(edit(kind, m, replicas, editAt, by, scaled))
	}
}

func revision(rs *appsv1.ReplicaSet) int {
	n, _ := strconv.Atoi(rs.Annotations[revisionAnnotation])
	return n
}

func images(s corev1.PodSpec) string {
	var out []string
	for _, c := range s.Containers {
		out = append(out, c.Image)
	}
	return "image " + strings.Join(out, ", ")
}

// templateDiff names what differs between two pod templates. Environment values
// are never printed.
func templateDiff(old, cur corev1.PodSpec) string {
	prev := map[string]corev1.Container{}
	for _, c := range old.Containers {
		prev[c.Name] = c
	}
	var out []string
	for _, c := range cur.Containers {
		p, ok := prev[c.Name]
		if !ok {
			out = append(out, "container "+c.Name+" added")
			continue
		}
		delete(prev, c.Name)
		if p.Image != c.Image {
			out = append(out, fmt.Sprintf("%s image %s -> %s", c.Name, p.Image, c.Image))
		}
		for what, same := range map[string]bool{
			"env":       reflect.DeepEqual(p.Env, c.Env) && reflect.DeepEqual(p.EnvFrom, c.EnvFrom),
			"resources": reflect.DeepEqual(p.Resources, c.Resources),
			"command":   reflect.DeepEqual(p.Command, c.Command) && reflect.DeepEqual(p.Args, c.Args),
			"probes":    reflect.DeepEqual(p.LivenessProbe, c.LivenessProbe) && reflect.DeepEqual(p.ReadinessProbe, c.ReadinessProbe),
		} {
			if !same {
				out = append(out, c.Name+" "+what+" changed")
			}
		}
	}
	for name := range prev {
		out = append(out, "container "+name+" removed")
	}
	if len(out) == 0 {
		return "pod template changed (annotations, volumes or other settings)"
	}
	sort.Strings(out)
	return strings.Join(out, "; ")
}

// events adds what only events record: autoscaling and replica changes.
func (f *finder) events(rows []events.Row) {
	for _, r := range rows {
		if !f.recent(r.Last) || (r.Reason != "ScalingReplicaSet" && r.Reason != "SuccessfulRescale") {
			continue
		}
		// a rollout or edit already listed for this object explains its events
		covered := false
		for _, c := range f.out {
			if c.Namespace == r.Namespace && c.Name == r.Name && c.Kind == r.Kind && c.Time.Sub(r.Last).Abs() <= near {
				covered = true
			}
		}
		if !covered {
			f.add(Change{Time: r.Last, Namespace: r.Namespace, Kind: r.Kind, Name: r.Name, Action: "scaled", Detail: why.Shorten(r.Message, 160)})
		}
	}
}

func (f *finder) nodes(rows []events.Row) {
	for _, r := range rows {
		switch r.Reason {
		case "Rebooted", "NodeNotReady", "NodeReady", "NodeNotSchedulable", "NodeSchedulable":
			if f.recent(r.Last) {
				f.add(Change{Time: r.Last, Kind: "Node", Name: r.Name, Action: "node", Detail: r.Reason})
			}
		}
	}
}
