// Package rm selects pods for bulk deletion.
package rm

import (
	"fmt"
	"path"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/beomzh/kubekaze/internal/podstatus"
)

// Criteria narrows which pods are selected. All set criteria must match
// (AND); the values inside Patterns and Statuses are alternatives (OR).
type Criteria struct {
	Patterns  []string      // glob patterns on pod name, e.g. "api-*"
	Statuses  []string      // computed status, case-insensitive, e.g. CrashLoopBackOff
	OlderThan time.Duration // 0 = any age
	Node      string        // exact node name
	Now       time.Time
}

// IsEmpty reports whether no criteria were given.
func (c Criteria) IsEmpty() bool {
	return len(c.Patterns) == 0 && len(c.Statuses) == 0 && c.OlderThan == 0 && c.Node == ""
}

// Validate checks the glob patterns.
func (c Criteria) Validate() error {
	for _, p := range c.Patterns {
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("invalid pattern %q: %w", p, err)
		}
	}
	return nil
}

// Select returns the pods matching c, preserving input order.
func Select(pods []corev1.Pod, c Criteria) []corev1.Pod {
	var out []corev1.Pod
	for i := range pods {
		if matches(&pods[i], c) {
			out = append(out, pods[i])
		}
	}
	return out
}

func matches(p *corev1.Pod, c Criteria) bool {
	if len(c.Patterns) > 0 {
		ok := false
		for _, pat := range c.Patterns {
			if m, _ := path.Match(pat, p.Name); m {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(c.Statuses) > 0 {
		st := podstatus.Of(p)
		ok := false
		for _, s := range c.Statuses {
			if strings.EqualFold(s, st) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if c.OlderThan > 0 && !c.Now.IsZero() {
		if p.CreationTimestamp.Time.After(c.Now.Add(-c.OlderThan)) {
			return false
		}
	}
	if c.Node != "" && p.Spec.NodeName != c.Node {
		return false
	}
	return true
}
