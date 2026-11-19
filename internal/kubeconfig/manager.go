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
