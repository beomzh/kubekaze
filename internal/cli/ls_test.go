package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/beomzh/kubekaze/internal/term"
)

func i32(n int32) *int32 { return &n }

func lsFixture() *fake.Clientset {
	meta := func(n string) metav1.ObjectMeta { return metav1.ObjectMeta{Namespace: "demo", Name: n} }
	return fake.NewSimpleClientset(
		&appsv1.Deployment{ObjectMeta: meta("api"), Spec: appsv1.DeploymentSpec{Replicas: i32(3)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}},
		&appsv1.Deployment{ObjectMeta: meta("web"), Spec: appsv1.DeploymentSpec{Replicas: i32(2)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 2}},
		&appsv1.StatefulSet{ObjectMeta: meta("db"), Spec: appsv1.StatefulSetSpec{Replicas: i32(1)}, Status: appsv1.StatefulSetStatus{ReadyReplicas: 1}},
		&batchv1.Job{ObjectMeta: meta("migrate"), Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue}}}},
		&batchv1.CronJob{ObjectMeta: meta("nightly"), Spec: batchv1.CronJobSpec{Schedule: "0 3 * * *"}},
		&corev1.Service{ObjectMeta: meta("api"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.1", Ports: []corev1.ServicePort{{Port: 80, Protocol: "TCP"}}}},
		&corev1.PersistentVolumeClaim{ObjectMeta: meta("data"), Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}},
		&corev1.ConfigMap{ObjectMeta: meta("app-config"), Data: map[string]string{"k": "v"}},
		&corev1.Secret{ObjectMeta: meta("db-credentials"), Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"password": []byte("hunter2-TOPSECRET")}, StringData: map[string]string{"token": "tok-TOPSECRET"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "other", Name: "x"}},
	)
}

func runLsApp(t *testing.T, cs *fake.Clientset, output string, args ...string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	a := &app{out: &out, err: &errb, pal: term.Palette{}, kc: cs, output: output}
	a.opts.Namespace = "demo"
	err := a.runLs(context.Background(), args)
	return out.String(), errb.String(), err
}

func TestLsOverview(t *testing.T) {
	out, _, err := runLsApp(t, lsFixture(), "text")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Deployments (2, 1 unhealthy)", "1/3", "StatefulSets (1)", "migrate", "Failed", "nightly", "Pending", "app-config", "db-credentials", "none: daemonsets"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "api") > strings.Index(out, "web") {
		t.Errorf("unhealthy deployment should be first:\n%s", out)
	}
	if strings.Contains(out, "other") || strings.Contains(out, "\nx") {
		t.Errorf("other namespace leaked:\n%s", out)
	}
}

func TestLsNeverPrintsSecretData(t *testing.T) {
	for _, o := range []string{"text", "json"} {
		for _, args := range [][]string{nil, {"secret"}, {"secret", "db-credentials"}} {
			out, errs, err := runLsApp(t, lsFixture(), o, args...)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out+errs, "TOPSECRET") || strings.Contains(out+errs, "hunter2") || strings.Contains(out, "aHVudGVy") {
				t.Errorf("secret value leaked (output %s, args %v): %s", o, args, out)
			}
			if !strings.Contains(out, "db-credentials") {
				t.Errorf("secret name missing (output %s, args %v)", o, args)
			}
		}
	}
}

func TestLsDetailAndAliases(t *testing.T) {
	for _, k := range []string{"deploy", "Deployments", "deployment"} {
		out, _, err := runLsApp(t, lsFixture(), "text", k)
		if err != nil || !strings.Contains(out, "UP-TO-DATE") || !strings.Contains(out, "web") {
			t.Errorf("%s: err=%v out=%s", k, err, out)
		}
	}
	out, _, err := runLsApp(t, lsFixture(), "text", "deploy", "web")
	if err != nil || strings.Contains(out, "api") || !strings.Contains(out, "web") {
		t.Errorf("name filter: %v\n%s", err, out)
	}
	if _, _, err := runLsApp(t, lsFixture(), "text", "deploy", "nope"); err == nil {
		t.Error("want error for unknown name")
	}
	if _, _, err := runLsApp(t, lsFixture(), "text", "bogus"); err == nil {
		t.Error("want error for unknown kind")
	}
}

func TestLsAllNamespacesAndJSON(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out, err: &bytes.Buffer{}, pal: term.Palette{}, kc: lsFixture(), output: "text"}
	a.opts.AllNamespaces = true
	if err := a.runLs(context.Background(), []string{"secret"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Secrets (2)") || !strings.Contains(out.String(), "NAMESPACE") || !strings.Contains(out.String(), "other") {
		t.Errorf("all namespaces:\n%s", out.String())
	}
	js, _, err := runLsApp(t, lsFixture(), "json")
	if err != nil || !strings.Contains(js, `"kind": "deployments"`) || !strings.Contains(js, `"healthy": false`) {
		t.Errorf("json: %v\n%s", err, js)
	}
}

func TestLsLargeSectionSummarised(t *testing.T) {
	var objs []runtime.Object
	for i := 0; i < 12; i++ {
		objs = append(objs, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: fmt.Sprintf("ok-%02d", i)}, Spec: appsv1.DeploymentSpec{Replicas: i32(1)}, Status: appsv1.DeploymentStatus{ReadyReplicas: 1}})
	}
	objs = append(objs, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "broken"}, Spec: appsv1.DeploymentSpec{Replicas: i32(2)}})
	out, _, _ := runLsApp(t, fake.NewSimpleClientset(objs...), "text")
	if !strings.Contains(out, "broken") || strings.Contains(out, "ok-03") || !strings.Contains(out, "12 healthy, not listed") {
		t.Errorf("summary:\n%s", out)
	}
}

func TestLsForbiddenIsSkippedWithNote(t *testing.T) {
	cs := lsFixture()
	cs.PrependReactor("list", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", fmt.Errorf("no"))
	})
	out, errs, err := runLsApp(t, cs, "text")
	if err != nil || !strings.Contains(out, "Deployments") || !strings.Contains(errs, "skipped Secrets (forbidden)") {
		t.Errorf("err=%v out=%s errs=%s", err, out, errs)
	}
	if _, _, err := runLsApp(t, cs, "text", "secret"); err == nil {
		t.Error("detail of a forbidden kind should fail")
	}
}

func TestParseRoutes(t *testing.T) {
	items, err := parseRoutes([]byte(`{"items":[{"metadata":{"name":"r","namespace":"demo"},"spec":{"host":"a.example.com","to":{"name":"api"}},"status":{"ingress":[{"conditions":[{"type":"Admitted","status":"False"}]}]}}]}`))
	if err != nil || len(items) != 1 || items[0].Healthy || items[0].cols[0] != "a.example.com" {
		t.Errorf("%v %+v", err, items)
	}
}

func TestLsSanitisesClusterStrings(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "evil\x1b[2J"}})
	out, _, _ := runLsApp(t, cs, "text")
	if strings.Contains(out, "\x1b") {
		t.Errorf("escape leaked: %q", out)
	}
}

func TestLsCompletion(t *testing.T) {
	a := &app{kc: lsFixture(), pal: term.Palette{}}
	a.opts.Namespace = "demo"
	cmd := a.newLsCmd()
	got, _ := a.completeLsArgs(cmd, nil, "s")
	if strings.Join(got, ",") != "sts,svc,secret" {
		t.Errorf("kinds: %v", got)
	}
	got, _ = a.completeLsArgs(cmd, []string{"deploy"}, "a")
	if len(got) != 1 || got[0] != "api" {
		t.Errorf("names: %v", got)
	}
}
