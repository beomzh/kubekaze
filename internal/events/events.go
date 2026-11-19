// Package events turns raw Kubernetes events into sorted, collapsed and
// summarised views.
package events

import (
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Row is a flattened event.
type Row struct {
	Namespace string    `json:"namespace"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Message   string    `json:"message"`
	Count     int       `json:"count"`
	First     time.Time `json:"firstSeen"`
	Last      time.Time `json:"lastSeen"`
	// Merged is the number of raw events folded into this row (1 = none).
	Merged int `json:"merged"`
}

// Object returns "kind/name" in lower case kind form.
func (r Row) Object() string {
	return strings.ToLower(r.Kind) + "/" + r.Name
}

// FromCore converts a core/v1 Event.
func FromCore(e corev1.Event) Row {
	last := e.LastTimestamp.Time
	if last.IsZero() {
		last = e.EventTime.Time
	}
	if last.IsZero() && e.Series != nil {
		last = e.Series.LastObservedTime.Time
	}
	if last.IsZero() {
		last = e.CreationTimestamp.Time
	}
	first := e.FirstTimestamp.Time
	if first.IsZero() {
		first = e.EventTime.Time
	}
	if first.IsZero() {
		first = last
	}
	count := int(e.Count)
	if e.Series != nil && int(e.Series.Count) > count {
		count = int(e.Series.Count)
	}
	if count < 1 {
		count = 1
	}
	ns := e.Namespace
	if ns == "" {
		ns = e.InvolvedObject.Namespace
	}
	return Row{
		Namespace: ns,
		Type:      e.Type,
		Reason:    e.Reason,
		Kind:      e.InvolvedObject.Kind,
		Name:      e.InvolvedObject.Name,
		Message:   strings.TrimSpace(e.Message),
		Count:     count,
		First:     first,
		Last:      last,
		Merged:    1,
	}
}

// Filter selects rows.
type Filter struct {
	Types   []string // e.g. Warning, Normal (case-insensitive)
	Reasons []string // case-insensitive
	Since   time.Duration
	Now     time.Time
	Kind    string // optional, from --for (already normalised, lower case)
	Name    string // optional
}

// Apply returns the rows matching f.
func (f Filter) Apply(rows []Row) []Row {
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		if len(f.Types) > 0 && !containsFold(f.Types, r.Type) {
			continue
		}
		if len(f.Reasons) > 0 && !containsFold(f.Reasons, r.Reason) {
			continue
		}
		if f.Since > 0 && !f.Now.IsZero() && r.Last.Before(f.Now.Add(-f.Since)) {
			continue
		}
		if f.Kind != "" && !strings.EqualFold(r.Kind, f.Kind) {
			continue
		}
		if f.Name != "" && r.Name != f.Name {
			continue
		}
		out = append(out, r)
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// Collapse folds events that differ only by timing into one row, summing
// counts and widening the time range.
func Collapse(rows []Row) []Row {
	type key struct{ ns, typ, reason, kind, name, msg string }
	idx := map[key]int{}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		k := key{r.Namespace, r.Type, r.Reason, r.Kind, r.Name, r.Message}
		i, ok := idx[k]
		if !ok {
			idx[k] = len(out)
			out = append(out, r)
			continue
		}
		m := &out[i]
		m.Count += r.Count
		m.Merged += r.Merged
		if r.First.Before(m.First) {
			m.First = r.First
		}
		if r.Last.After(m.Last) {
			m.Last = r.Last
		}
	}
	return out
}

// SortKeys are the valid values for Sort.
var SortKeys = []string{"time", "count", "type", "namespace", "object", "reason"}

// Sort orders rows in place. "time" sorts by last-seen ascending, so the
// newest event is last (at the bottom of the terminal). Set reverse to flip.
func Sort(rows []Row, key string, reverse bool) {
	less := func(a, b Row) bool { return a.Last.Before(b.Last) }
	switch key {
	case "count":
		less = func(a, b Row) bool { return a.Count < b.Count }
	case "type":
		// Warnings first, then by time.
		less = func(a, b Row) bool {
			if a.Type != b.Type {
				return a.Type == "Warning"
			}
			return a.Last.Before(b.Last)
		}
	case "namespace":
		less = func(a, b Row) bool {
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
			return a.Last.Before(b.Last)
		}
	case "object":
		less = func(a, b Row) bool {
			if ao, bo := a.Object(), b.Object(); ao != bo {
				return ao < bo
			}
			return a.Last.Before(b.Last)
		}
	case "reason":
		less = func(a, b Row) bool {
			if a.Reason != b.Reason {
				return a.Reason < b.Reason
			}
			return a.Last.Before(b.Last)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if reverse {
			return less(rows[j], rows[i])
		}
		return less(rows[i], rows[j])
	})
}

// NamespaceSummary is a one-line overview of a namespace.
type NamespaceSummary struct {
	Namespace  string    `json:"namespace"`
	Events     int       `json:"events"`
	Warnings   int       `json:"warnings"`
	TopReasons []string  `json:"topReasons"`
	Last       time.Time `json:"lastSeen"`
}

// Summarize groups rows per namespace. Event counts use the occurrence count,
// so repeated events weigh what they should. Result is ordered by warnings
// (most first), then name.
func Summarize(rows []Row) []NamespaceSummary {
	type acc struct {
		sum     NamespaceSummary
		reasons map[string]int
	}
	m := map[string]*acc{}
	for _, r := range rows {
		a := m[r.Namespace]
		if a == nil {
			a = &acc{sum: NamespaceSummary{Namespace: r.Namespace}, reasons: map[string]int{}}
			m[r.Namespace] = a
		}
		a.sum.Events += r.Count
		if r.Type == "Warning" {
			a.sum.Warnings += r.Count
			a.reasons[r.Reason] += r.Count
		}
		if r.Last.After(a.sum.Last) {
			a.sum.Last = r.Last
		}
	}
	out := make([]NamespaceSummary, 0, len(m))
	for _, a := range m {
		type rc struct {
			reason string
			n      int
		}
		rcs := make([]rc, 0, len(a.reasons))
		for k, v := range a.reasons {
			rcs = append(rcs, rc{k, v})
		}
		sort.Slice(rcs, func(i, j int) bool {
			if rcs[i].n != rcs[j].n {
				return rcs[i].n > rcs[j].n
			}
			return rcs[i].reason < rcs[j].reason
		})
		for i := 0; i < len(rcs) && i < 3; i++ {
			a.sum.TopReasons = append(a.sum.TopReasons, rcs[i].reason)
		}
		out = append(out, a.sum)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Warnings != out[j].Warnings {
			return out[i].Warnings > out[j].Warnings
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}

// ParseSince parses durations such as 30m, 2h or 1d.
func ParseSince(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		d, err := time.ParseDuration(strings.TrimSuffix(s, "d") + "h")
		return d * 24, err
	}
	return time.ParseDuration(s)
}

// Age formats a duration compactly (e.g. 45s, 12m, 3h, 2d).
func Age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < 2*time.Minute:
		return itoa(int(d.Seconds())) + "s"
	case d < 2*time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
