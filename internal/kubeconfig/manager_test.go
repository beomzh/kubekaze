package kubeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `apiVersion: v1
kind: Config
current-context: dev
clusters:
- name: c1
  cluster: {server: "https://127.0.0.1:1"}
users:
- name: u1
  user: {token: "x"}
contexts:
- name: dev
  context: {cluster: c1, user: u1, namespace: app}
- name: prod
  context: {cluster: c1, user: u1}
`

func newManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	kc := filepath.Join(dir, "config")
	if err := os.WriteFile(kc, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	return New(kc, filepath.Join(dir, "state.json")), kc
}

func TestContextSwitchAndPrevious(t *testing.T) {
	m, _ := newManager(t)
	names, cur, err := m.Contexts()
	if err != nil || cur != "dev" || len(names) != 2 {
		t.Fatalf("Contexts() = %v %q %v", names, cur, err)
	}
	if _, err := m.UseContext("prod"); err != nil {
		t.Fatal(err)
	}
	if _, cur, _ = m.Contexts(); cur != "prod" {
		t.Fatalf("current = %q, want prod", cur)
	}
	if got, err := m.UseContext("-"); err != nil || got != "dev" {
		t.Fatalf("UseContext(-) = %q, %v", got, err)
	}
	if _, err := m.UseContext("nope"); err == nil {
		t.Fatal("expected error for unknown context")
	}
}

func TestNamespaceSwitchAndPrevious(t *testing.T) {
	m, kc := newManager(t)
	if _, ns, _ := m.CurrentNamespace(); ns != "app" {
		t.Fatalf("namespace = %q, want app", ns)
	}
	if _, ns, err := m.UseNamespace("payments"); err != nil || ns != "payments" {
		t.Fatalf("UseNamespace = %q, %v", ns, err)
	}
	if _, ns, err := m.UseNamespace("-"); err != nil || ns != "app" {
		t.Fatalf("UseNamespace(-) = %q, %v", ns, err)
	}
	// File must stay private and other fields intact.
	info, err := os.Stat(kc)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeUnixPerm(info) != 0 && info.Mode().Perm()&0o077 != 0 {
		t.Errorf("kubeconfig permissions widened: %v", info.Mode().Perm())
	}
	names, _, _ := m.Contexts()
	if len(names) != 2 {
		t.Errorf("contexts changed: %v", names)
	}
}

func TestGroupContexts(t *testing.T) {
	infos := []ContextInfo{
		{Name: "grafana/api:6443/admin", Cluster: "api:6443", User: "admin", Namespace: "grafana"},
		{Name: "default/api:6443/admin", Cluster: "api:6443", User: "admin", Namespace: "default"},
		{Name: "istio/api:6443/admin", Cluster: "api:6443", User: "admin", Namespace: "istio"},
		{Name: "grafana/api:6443/dev", Cluster: "api:6443", User: "dev", Namespace: "grafana"},
		{Name: "kind-test", Cluster: "kind-test", User: "kind-test"},
	}
	g := GroupContexts(infos, "")
	if len(g) != 3 {
		t.Fatalf("groups = %d, want 3: %+v", len(g), g)
	}
	if g[0].User != "admin" || g[0].Count != 3 || g[0].Rep.Name != "default/api:6443/admin" {
		t.Errorf("admin group should pick the default namespace: %+v", g[0])
	}
	if g[1].User != "dev" || g[1].Count != 1 || g[1].Rep.Name != "grafana/api:6443/dev" {
		t.Errorf("single context stays as is: %+v", g[1])
	}
	cur := GroupContexts(infos, "istio/api:6443/admin")
	if !cur[0].Current || cur[0].Rep.Name != "istio/api:6443/admin" {
		t.Errorf("the current context must represent its group: %+v", cur[0])
	}
}
