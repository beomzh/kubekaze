// Package kube builds Kubernetes clients from the user's existing kubeconfig.
// It never implements its own authentication: client-go (and kubeconfig exec
// plugins for EKS, AKS, GKE, ...) do that.
package kube

import (
	"fmt"
	"os"
	"regexp"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/beomzh/kubekaze/internal/version"
)

// Options are the global targeting flags shared by all commands.
type Options struct {
	Kubeconfig    string
	Context       string
	Namespace     string
	AllNamespaces bool
}

func (o *Options) loader() clientcmd.ClientConfig {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if o.Kubeconfig != "" {
		rules.ExplicitPath = o.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{}
	if o.Context != "" {
		overrides.CurrentContext = o.Context
	}
	if o.Namespace != "" {
		overrides.Context.Namespace = o.Namespace
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
}

// REST returns the REST config for the selected context.
func (o *Options) REST() (*rest.Config, error) {
	cfg, err := o.loader().ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	cfg.UserAgent = "kubekaze/" + version.Version
	if cfg.QPS == 0 {
		cfg.QPS = 20
		cfg.Burst = 40
	}
	return cfg, nil
}

// Clientset returns a typed client for the selected context.
func (o *Options) Clientset() (kubernetes.Interface, error) {
	cfg, err := o.REST()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

// ContextName returns the effective context name.
func (o *Options) ContextName() (string, error) {
	if o.Context != "" {
		return o.Context, nil
	}
	raw, err := o.loader().RawConfig()
	if err != nil {
		return "", fmt.Errorf("loading kubeconfig: %w", err)
	}
	return raw.CurrentContext, nil
}

// ResolvedNamespace returns the namespace commands should target:
// the -n flag, else the context's namespace, else "default".
func (o *Options) ResolvedNamespace() (string, error) {
	ns, _, err := o.loader().Namespace()
	if err != nil {
		return "", fmt.Errorf("resolving namespace: %w", err)
	}
	return ns, nil
}

// NamespaceOrAll returns metav1.NamespaceAll ("") when -A is set.
func (o *Options) NamespaceOrAll() (string, error) {
	if o.AllNamespaces {
		return "", nil
	}
	return o.ResolvedNamespace()
}

var defaultProd = regexp.MustCompile(`(?i)(^|[-_./:])(prod|prd|production|live)([-_./:]|$)`)

// IsProduction reports whether a context name looks like production.
// Override with KUBEKAZE_PROD_PATTERN (a Go regular expression).
func IsProduction(contextName string) bool {
	if p := os.Getenv("KUBEKAZE_PROD_PATTERN"); p != "" {
		if re, err := regexp.Compile(p); err == nil {
			return re.MatchString(contextName)
		}
	}
	return defaultProd.MatchString(contextName)
}
