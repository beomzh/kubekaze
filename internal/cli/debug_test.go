package cli

import "testing"

func TestCheckDebugImage(t *testing.T) {
	good := []string{DefaultDebugImage, "nicolaka/netshoot:v0.13", "registry.local:5000/tools/dbg:1.2", "alpine@sha256:abc"}
	bad := []string{"", "busybox", "busybox:latest", "registry.local:5000/tools/dbg", "ubuntu:"}
	for _, g := range good {
		if err := checkDebugImage(g); err != nil {
			t.Errorf("checkDebugImage(%q) = %v, want ok", g, err)
		}
	}
	for _, b := range bad {
		if err := checkDebugImage(b); err == nil {
			t.Errorf("checkDebugImage(%q) accepted a mutable image", b)
		}
	}
}

func TestBuildDebugContainerIsUnprivileged(t *testing.T) {
	ec := buildDebugContainer("dbg", "img:1", "app")
	sc := ec.SecurityContext
	if sc == nil || *sc.Privileged || *sc.AllowPrivilegeEscalation {
		t.Fatal("debug container must not be privileged")
	}
	if len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
		t.Error("must drop all capabilities")
	}
	if ec.TargetContainerName != "app" || !ec.TTY || !ec.Stdin {
		t.Errorf("unexpected spec: %+v", ec)
	}
}

func TestDebugImageFrom(t *testing.T) {
	t.Setenv("KUBEKAZE_DEBUG_IMAGE", "")
	if debugImageFrom("") != DefaultDebugImage {
		t.Error("default expected")
	}
	t.Setenv("KUBEKAZE_DEBUG_IMAGE", "env:1")
	if debugImageFrom("") != "env:1" || debugImageFrom("flag:2") != "flag:2" {
		t.Error("env/flag precedence wrong")
	}
}
