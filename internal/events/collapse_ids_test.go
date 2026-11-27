package events

import (
	"testing"
	"time"
)

func failed(msg string, at time.Time) Row {
	return Row{Namespace: "a", Type: "Warning", Reason: "Failed", Kind: "Pod", Name: "p", Message: msg, Count: 1, Merged: 1, First: at, Last: at}
}

func TestCollapseIgnoresThrowawayIDs(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := func(attempt, id, layer, link string) string {
		return "failed to mount container k8s_app_p_a_a8fa7790-1111-2222-3333-444455556666_" + attempt + "(" + id + "): /var/lib/containers/storage/overlay/" + layer + "/merged lowerdir=/x/l/" + link + ": input/output error"
	}
	got := Collapse([]Row{
		failed(msg("1", "f35d7dc4", "86e74a92", "SF27MLJUOLXVYWZGLATMDHWZ7G"), t0),
		failed(msg("2", "0b1c2d3e", "99aa88bb", "DVH4VWICPKMGJAGVPFSATFQGEW"), t0.Add(time.Minute)),
	})
	if len(got) != 1 || got[0].Count != 2 || got[0].Message != msg("2", "0b1c2d3e", "99aa88bb", "DVH4VWICPKMGJAGVPFSATFQGEW") {
		t.Fatalf("got %+v", got)
	}
}

func TestCollapseKeepsDifferentContainersAndImagesApart(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	got := Collapse([]Row{
		failed("failed to mount container k8s_app_p_a_a8fa7790-1111-2222-3333-444455556666_1", t0),
		failed("failed to mount container k8s_sidecar_p_a_a8fa7790-1111-2222-3333-444455556666_1", t0),
		failed("pull img@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa failed", t0),
		failed("pull img@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb failed", t0),
		failed("Scaled up replica set api-7d9f8b6c5d to 1", t0),
		failed("Scaled up replica set api-5c6b7d8e9f to 1", t0),
	})
	if len(got) != 6 {
		t.Fatalf("want 6 rows, got %+v", got)
	}
}
