// Package usage turns metrics.k8s.io responses plus node and pod specs into
// a usage snapshot: nodes vs allocatable, pods vs requests/limits, and
// per-namespace totals. It does no I/O.
package usage

import (
	"encoding/json"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// HighPercent is the usage percentage above which a value is highlighted.
const HighPercent = 80

// Sort keys for pods and namespaces.
const (
	SortCPU    = "cpu"
	SortMemory = "memory"
)

type rawUsage struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type nodeMetricsList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Usage rawUsage `json:"usage"`
	} `json:"items"`
}

type podMetricsList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Containers []struct {
			Name  string   `json:"name"`
			Usage rawUsage `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// Node is one node's usage against what it can allocate.
type Node struct {
	Name          string  `json:"name"`
	CPUMilli      int64   `json:"cpuMilli"`
	CPUAllocMilli int64   `json:"cpuAllocatableMilli"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemBytes      int64   `json:"memoryBytes"`
	MemAllocBytes int64   `json:"memoryAllocatableBytes"`
	MemPercent    float64 `json:"memoryPercent"`
}

// Pod is one pod's usage against its requests and limits (0 = not set).
type Pod struct {
	Namespace     string  `json:"namespace"`
	Name          string  `json:"name"`
	CPUMilli      int64   `json:"cpuMilli"`
	MemBytes      int64   `json:"memoryBytes"`
	CPURequest    int64   `json:"cpuRequestMilli"`
	CPULimit      int64   `json:"cpuLimitMilli"`
	MemRequest    int64   `json:"memoryRequestBytes"`
	MemLimit      int64   `json:"memoryLimitBytes"`
	CPULimitPct   float64 `json:"cpuLimitPercent"`
	MemLimitPct   float64 `json:"memoryLimitPercent"`
	CPURequestPct float64 `json:"cpuRequestPercent"`
	MemRequestPct float64 `json:"memoryRequestPercent"`
}

// Namespace is the summed usage of the pods in one namespace.
type Namespace struct {
	Name     string `json:"name"`
	Pods     int    `json:"pods"`
	CPUMilli int64  `json:"cpuMilli"`
	MemBytes int64  `json:"memoryBytes"`
}

// Snapshot is the result of one measurement.
type Snapshot struct {
	Nodes      []Node      `json:"nodes,omitempty"`
	Pods       []Pod       `json:"pods,omitempty"`
	Namespaces []Namespace `json:"namespaces,omitempty"`
}

func pct(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

func parse(u rawUsage) (milli, mem int64, err error) {
	if u.CPU != "" {
		q, err := resource.ParseQuantity(u.CPU)
		if err != nil {
			return 0, 0, fmt.Errorf("cpu %q: %w", u.CPU, err)
		}
		milli = q.MilliValue()
	}
	if u.Memory != "" {
		q, err := resource.ParseQuantity(u.Memory)
		if err != nil {
			return 0, 0, fmt.Errorf("memory %q: %w", u.Memory, err)
		}
		mem = q.Value()
	}
	return milli, mem, nil
}

// Nodes joins NodeMetricsList JSON with node allocatable and sorts by the
// higher of CPU and memory percent, highest first.
func Nodes(metrics []byte, nodes []corev1.Node) ([]Node, error) {
	var l nodeMetricsList
	if err := json.Unmarshal(metrics, &l); err != nil {
		return nil, fmt.Errorf("decoding node metrics: %w", err)
	}
	alloc := map[string]corev1.ResourceList{}
	for _, n := range nodes {
		alloc[n.Name] = n.Status.Allocatable
	}
	out := make([]Node, 0, len(l.Items))
	for _, it := range l.Items {
		c, m, err := parse(it.Usage)
		if err != nil {
			return nil, fmt.Errorf("node %s: %w", it.Metadata.Name, err)
		}
		n := Node{Name: it.Metadata.Name, CPUMilli: c, MemBytes: m}
		if a, ok := alloc[n.Name]; ok {
			n.CPUAllocMilli = a.Cpu().MilliValue()
			n.MemAllocBytes = a.Memory().Value()
		}
		n.CPUPercent = pct(c, n.CPUAllocMilli)
		n.MemPercent = pct(m, n.MemAllocBytes)
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		hi, hj := maxf(out[i].CPUPercent, out[i].MemPercent), maxf(out[j].CPUPercent, out[j].MemPercent)
		if hi != hj {
			return hi > hj
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func sumContainers(spec []corev1.Container) (cpuReq, cpuLim, memReq, memLim int64) {
	for _, c := range spec {
		cpuReq += c.Resources.Requests.Cpu().MilliValue()
		cpuLim += c.Resources.Limits.Cpu().MilliValue()
		memReq += c.Resources.Requests.Memory().Value()
		memLim += c.Resources.Limits.Memory().Value()
	}
	return
}

// Pods joins PodMetricsList JSON with pod specs (for requests/limits). A
// limit is only reported when every container sets one, otherwise the sum
// would understate what the pod may use.
func Pods(metrics []byte, pods []corev1.Pod) ([]Pod, error) {
	var l podMetricsList
	if err := json.Unmarshal(metrics, &l); err != nil {
		return nil, fmt.Errorf("decoding pod metrics: %w", err)
	}
	type key struct{ ns, name string }
	specs := map[key]corev1.PodSpec{}
	for _, p := range pods {
		specs[key{p.Namespace, p.Name}] = p.Spec
	}
	out := make([]Pod, 0, len(l.Items))
	for _, it := range l.Items {
		p := Pod{Namespace: it.Metadata.Namespace, Name: it.Metadata.Name}
		for _, c := range it.Containers {
			cm, mm, err := parse(c.Usage)
			if err != nil {
				return nil, fmt.Errorf("pod %s/%s: %w", p.Namespace, p.Name, err)
			}
			p.CPUMilli += cm
			p.MemBytes += mm
		}
		if spec, ok := specs[key{p.Namespace, p.Name}]; ok {
			p.CPURequest, p.CPULimit, p.MemRequest, p.MemLimit = sumContainers(spec.Containers)
			for _, c := range spec.Containers {
				if c.Resources.Limits.Cpu().IsZero() {
					p.CPULimit = 0
				}
				if c.Resources.Limits.Memory().IsZero() {
					p.MemLimit = 0
				}
			}
		}
		p.CPULimitPct = pct(p.CPUMilli, p.CPULimit)
		p.MemLimitPct = pct(p.MemBytes, p.MemLimit)
		p.CPURequestPct = pct(p.CPUMilli, p.CPURequest)
		p.MemRequestPct = pct(p.MemBytes, p.MemRequest)
		out = append(out, p)
	}
	return out, nil
}

// SortPods orders pods by the key (cpu or memory), highest first.
func SortPods(pods []Pod, key string) {
	sort.SliceStable(pods, func(i, j int) bool {
		a, b := pods[i], pods[j]
		x, y := a.CPUMilli, b.CPUMilli
		if key == SortMemory {
			x, y = a.MemBytes, b.MemBytes
		}
		if x != y {
			return x > y
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
}

// ByNamespace sums pod usage per namespace, highest first by key.
func ByNamespace(pods []Pod, key string) []Namespace {
	m := map[string]*Namespace{}
	for _, p := range pods {
		n := m[p.Namespace]
		if n == nil {
			n = &Namespace{Name: p.Namespace}
			m[p.Namespace] = n
		}
		n.Pods++
		n.CPUMilli += p.CPUMilli
		n.MemBytes += p.MemBytes
	}
	out := make([]Namespace, 0, len(m))
	for _, n := range m {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool {
		x, y := out[i].CPUMilli, out[j].CPUMilli
		if key == SortMemory {
			x, y = out[i].MemBytes, out[j].MemBytes
		}
		if x != y {
			return x > y
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Limit returns at most n items of s (n <= 0 means all).
func Limit[T any](s []T, n int) []T {
	if n > 0 && len(s) > n {
		return s[:n]
	}
	return s
}

// FormatCPU renders millicores as "250m" or "1.5" cores when 1000m or more.
func FormatCPU(milli int64) string {
	if milli >= 1000 {
		if milli%1000 == 0 {
			return fmt.Sprintf("%d", milli/1000)
		}
		return fmt.Sprintf("%.2f", float64(milli)/1000)
	}
	return fmt.Sprintf("%dm", milli)
}

// FormatMem renders bytes as Ki, Mi or Gi.
func FormatMem(b int64) string {
	const mi, gi = 1 << 20, 1 << 30
	switch {
	case b >= gi:
		return fmt.Sprintf("%.1fGi", float64(b)/gi)
	case b >= mi:
		return fmt.Sprintf("%dMi", b/mi)
	default:
		return fmt.Sprintf("%dKi", b/1024)
	}
}
