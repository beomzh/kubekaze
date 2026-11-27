package changes

import (
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/beomzh/kubekaze/internal/events"
)

var now = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func at(ago time.Duration) metav1.Time { return metav1.NewTime(now.Add(-ago)) }

func meta(name string, age time.Duration, edits ...metav1.ManagedFieldsEntry) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: "shop", CreationTimestamp: at(age), ManagedFields: edits}
}

func wrote(manager, field, subresource string, ago time.Duration) metav1.ManagedFieldsEntry {
	t := at(ago)
	return metav1.ManagedFieldsEntry{Manager: manager, Time: &t, Subresource: subresource,
		FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"` + field + `":{}}`)}}
}

func find(in Input) []Change {
	in.Since = now.Add(-time.Hour)
	return Find(in)
}

func dump(cs []Change) string {
	var b strings.Builder
	for _, c := range cs {
		fmt.Fprintf(&b, "%s %s %s: %s by %s\n", c.Kind, c.Name, c.Action, c.Detail, c.By)
	}
	return b.String()
}

func replicaSet(name, rev, image, secretEnv string, age time.Duration) appsv1.ReplicaSet {
	m := meta(name, age)
	m.OwnerReferences = []metav1.OwnerReference{{Kind: "Deployment", Name: "api"}}
	m.Annotations = map[string]string{revisionAnnotation: rev}
	return appsv1.ReplicaSet{ObjectMeta: m, Spec: appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
		Containers: []corev1.Container{{Name: "app", Image: image, Env: []corev1.EnvVar{{Name: "TOKEN", Value: secretEnv}}}}}}}}
}

func TestRolloutNamesImageChangeAndNeverEnvValues(t *testing.T) {
	got := find(Input{
		Deployments: []appsv1.Deployment{{ObjectMeta: meta("api", 48*time.Hour, wrote("kubectl-set", "f:spec", "", 10*time.Minute))}},
		ReplicaSets: []appsv1.ReplicaSet{replicaSet("api-2", "2", "api:v2", "new-secret", 10*time.Minute), replicaSet("api-1", "1", "api:v1", "old-secret", 48*time.Hour)},
	})
	out := dump(got)
	if len(got) != 1 || !strings.Contains(out, "app image api:v1 -> api:v2") || !strings.Contains(out, "app env changed") || got[0].By != "kubectl-set" {
		t.Fatalf("got %s", out)
	}
	if strings.Contains(out, "secret") {
		t.Errorf("env values must not be printed: %s", out)
	}
}

func TestConfigAndSecretEditsIgnoreStatusClusterNoiseAndOldOnes(t *testing.T) {
	got := find(Input{
		ConfigMaps: []corev1.ConfigMap{
			{ObjectMeta: meta("app-config", 48*time.Hour, wrote("kubectl-edit", "f:data", "", 5*time.Minute))},
			{ObjectMeta: meta("kube-root-ca.crt", 48*time.Hour, wrote("x", "f:data", "", 5*time.Minute))},
			{ObjectMeta: meta("labels-only", 48*time.Hour, wrote("x", "f:metadata", "", 5*time.Minute))},
			{ObjectMeta: meta("old", 48*time.Hour, wrote("x", "f:data", "", 5*time.Hour))},
		},
		Secrets: []corev1.Secret{
			{ObjectMeta: meta("db", 48*time.Hour, wrote("argocd", "f:data", "", 3*time.Minute)), Data: map[string][]byte{"password": []byte("hunter2")}},
			{ObjectMeta: meta("token", time.Minute), Type: corev1.SecretTypeServiceAccountToken},
		},
		Services: []corev1.Service{{ObjectMeta: meta("lb", 48*time.Hour, wrote("cloud", "f:status", "status", time.Minute))}},
	})
	out := dump(got)
	if len(got) != 2 || got[0].Name != "app-config" || got[1].Name != "db" {
		t.Fatalf("want app-config then db (oldest first), got %s", out)
	}
	if strings.Contains(out, "hunter2") || strings.Contains(out, "password") {
		t.Errorf("secret content leaked: %s", out)
	}
}

func TestScaleCreatedHelmAndNodes(t *testing.T) {
	three := int32(3)
	rev := func(v, status string, age time.Duration) corev1.Secret {
		m := meta("sh.helm.release.v1.web.v"+v, age)
		m.Labels = map[string]string{"name": "web", "version": v, "status": status}
		return corev1.Secret{ObjectMeta: m, Type: helmSecretType}
	}
	got := find(Input{
		Deployments: []appsv1.Deployment{
			{ObjectMeta: meta("api", 48*time.Hour, wrote("kubectl", "f:spec", "scale", 20*time.Minute)), Spec: appsv1.DeploymentSpec{Replicas: &three}},
			{ObjectMeta: meta("fresh", 15*time.Minute)},
		},
		Secrets: []corev1.Secret{rev("7", "superseded", 12*time.Minute), rev("8", "deployed", 8*time.Minute)},
		Events: []events.Row{
			{Namespace: "shop", Kind: "Deployment", Name: "api", Reason: "ScalingReplicaSet", Message: "Scaled up", Last: now.Add(-20 * time.Minute)},
			{Namespace: "shop", Kind: "HorizontalPodAutoscaler", Name: "web", Reason: "SuccessfulRescale", Message: "New size: 5", Last: now.Add(-2 * time.Minute)},
		},
		NodeEvents: []events.Row{{Kind: "Node", Name: "n1", Reason: "Rebooted", Last: now.Add(-30 * time.Minute)}, {Kind: "Node", Name: "n1", Reason: "NodeHasNoDiskPressure", Last: now}},
	})
	out := dump(got)
	for _, want := range []string{"Node n1 node: Rebooted", "Deployment api scaled: now 3 replica(s)", "Deployment fresh created", "HelmRelease web rollout: revisions 7-8 (deployed)", "web scaled: New size: 5"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if len(got) != 5 {
		t.Errorf("scaling event of an already listed edit must not repeat:\n%s", out)
	}
}
