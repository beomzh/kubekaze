// Package kubeconfig switches the current context and namespace.
//
// It changes only "current-context" and the "namespace" field of a context,
// using client-go's standard writer (the same one "kubectl config use-context"
// uses), and remembers the previous values so "-" can switch back.
package kubeconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

// Manager reads and updates a kubeconfig.
type Manager struct {
	paths     *clientcmd.PathOptions
	statePath string
}

// New creates a Manager. kubeconfigPath may be empty to use KUBECONFIG/default.
// statePath is where previous values are remembered; empty disables that.
func New(kubeconfigPath, statePath string) *Manager {
	po := clientcmd.NewDefaultPathOptions()
	if kubeconfigPath != "" {
		po.LoadingRules.ExplicitPath = kubeconfigPath
	}
	return &Manager{paths: po, statePath: statePath}
}

// DefaultStatePath returns the per-user state file location.
func DefaultStatePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "kubekaze", "state.json")
}

type state struct {
	PrevContext    string            `json:"prevContext,omitempty"`
	PrevNamespaces map[string]string `json:"prevNamespaces,omitempty"`
}

func (m *Manager) loadState() state {
	var s state
	if m.statePath == "" {
		return s
	}
	if b, err := os.ReadFile(m.statePath); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func (m *Manager) saveState(s state) {
	if m.statePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o700); err != nil {
		return
	}
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	tmp := m.statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, m.statePath)
}

func (m *Manager) load() (*api.Config, error) {
	cfg, err := m.paths.GetStartingConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	return cfg, nil
}

// Contexts returns sorted context names and the current one.
func (m *Manager) Contexts() ([]string, string, error) {
	cfg, err := m.load()
	if err != nil {
		return nil, "", err
	}
	names := make([]string, 0, len(cfg.Contexts))
	for n := range cfg.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, cfg.CurrentContext, nil
}

// UseContext switches the current context. "-" switches to the previous one.
// It returns the context that is now active.
func (m *Manager) UseContext(name string) (string, error) {
	cfg, err := m.load()
	if err != nil {
		return "", err
	}
	st := m.loadState()
	if name == "-" {
		if st.PrevContext == "" {
			return "", errors.New("no previous context recorded")
		}
		name = st.PrevContext
	}
	if _, ok := cfg.Contexts[name]; !ok {
		return "", fmt.Errorf("context %q not found in kubeconfig", name)
	}
	if name == cfg.CurrentContext {
		return name, nil
	}
	st.PrevContext = cfg.CurrentContext
	cfg.CurrentContext = name
	if err := clientcmd.ModifyConfig(m.paths, *cfg, true); err != nil {
		return "", fmt.Errorf("writing kubeconfig: %w", err)
	}
	m.saveState(st)
	return name, nil
}

// CurrentNamespace returns the namespace recorded for the current context
// ("default" if none).
func (m *Manager) CurrentNamespace() (ctxName, ns string, err error) {
	cfg, err := m.load()
	if err != nil {
		return "", "", err
	}
	c, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return cfg.CurrentContext, "", errors.New("no current context set")
	}
	if c.Namespace == "" {
		return cfg.CurrentContext, "default", nil
	}
	return cfg.CurrentContext, c.Namespace, nil
}

// UseNamespace sets the namespace of the current context. "-" switches back to
// the previous namespace of that context. It returns the context and namespace.
func (m *Manager) UseNamespace(ns string) (string, string, error) {
	cfg, err := m.load()
	if err != nil {
		return "", "", err
	}
	c, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok {
		return "", "", errors.New("no current context set")
	}
	st := m.loadState()
	if ns == "-" {
		prev := st.PrevNamespaces[cfg.CurrentContext]
		if prev == "" {
			return "", "", errors.New("no previous namespace recorded for this context")
		}
		ns = prev
	}
	cur := c.Namespace
	if cur == "" {
		cur = "default"
	}
	if ns == cur {
		return cfg.CurrentContext, ns, nil
	}
	if st.PrevNamespaces == nil {
		st.PrevNamespaces = map[string]string{}
	}
	st.PrevNamespaces[cfg.CurrentContext] = cur
	c.Namespace = ns
	if err := clientcmd.ModifyConfig(m.paths, *cfg, true); err != nil {
		return "", "", fmt.Errorf("writing kubeconfig: %w", err)
	}
	m.saveState(st)
	return cfg.CurrentContext, ns, nil
}

// ContextInfo describes one kubeconfig context.
type ContextInfo struct {
	Name      string
	Cluster   string
	User      string
	Namespace string
}

// ContextInfos returns all contexts sorted by name, and the current one.
func (m *Manager) ContextInfos() ([]ContextInfo, string, error) {
	cfg, err := m.load()
	if err != nil {
		return nil, "", err
	}
	out := make([]ContextInfo, 0, len(cfg.Contexts))
	for name, c := range cfg.Contexts {
		out = append(out, ContextInfo{Name: name, Cluster: c.Cluster, User: c.AuthInfo, Namespace: c.Namespace})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, cfg.CurrentContext, nil
}

// Group is a set of contexts that share a cluster and a user, typically
// differing only by namespace (this is what "oc project" creates:
// <namespace>/<cluster>/<user>).
type Group struct {
	Cluster, User string
	Rep           ContextInfo // the context to switch to when the group is chosen
	Count         int
	Current       bool
}

// GroupContexts collapses contexts by cluster and user. The representative is
// the current context if it is in the group, else one in the "default"
// namespace (or without a namespace), else the first by name.
func GroupContexts(infos []ContextInfo, current string) []Group {
	type key struct{ cluster, user string }
	idx := map[key]int{}
	var groups []Group
	for _, c := range infos {
		k := key{c.Cluster, c.User}
		i, ok := idx[k]
		if !ok {
			idx[k] = len(groups)
			groups = append(groups, Group{Cluster: c.Cluster, User: c.User, Rep: c, Count: 1, Current: c.Name == current})
			continue
		}
		g := &groups[i]
		g.Count++
		switch {
		case c.Name == current:
			g.Rep, g.Current = c, true
		case g.Current:
			// keep the current context as representative
		case isDefaultNS(c.Namespace) && !isDefaultNS(g.Rep.Namespace):
			g.Rep = c
		}
	}
	return groups
}

func isDefaultNS(ns string) bool { return ns == "" || ns == "default" }
