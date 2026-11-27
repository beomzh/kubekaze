package cli

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestChangesListsRecentObjectsAndValidatesSince(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "fresh", Namespace: "default", CreationTimestamp: metav1.Now()}})
	var out, errs strings.Builder
	a := &app{out: &out, err: &errs, kc: cs}
	a.opts.Namespace = "default"
	if err := a.runChanges(context.Background(), "1h"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "fresh") || !strings.Contains(out.String(), "created") {
		t.Errorf("output:\n%s%s", out.String(), errs.String())
	}
	if err := a.runChanges(context.Background(), "soon"); err == nil || !strings.Contains(err.Error(), "--since") {
		t.Errorf("err = %v", err)
	}
}
