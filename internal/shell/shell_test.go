package shell

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func probeWith(available ...string) (Probe, *[]string) {
	var tried []string
	return func(_ context.Context, cmd []string) error {
		tried = append(tried, strings.Join(cmd, " "))
		for _, a := range available {
			if strings.HasPrefix(strings.Join(cmd, " "), a+" ") {
				return nil
			}
		}
		return errors.New("not found")
	}, &tried
}

func TestFindPrefersBash(t *testing.T) {
	p, _ := probeWith("bash", "sh")
	got, err := Find(context.Background(), p)
	if err != nil || !reflect.DeepEqual(got, []string{"bash"}) {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestFindFallsBackToShThenBusybox(t *testing.T) {
	p, tried := probeWith("sh")
	got, err := Find(context.Background(), p)
	if err != nil || !reflect.DeepEqual(got, []string{"sh"}) || len(*tried) != 2 {
		t.Errorf("got %v, %v, tried %v", got, err, *tried)
	}
	p, _ = probeWith("busybox")
	got, err = Find(context.Background(), p)
	if err != nil || !reflect.DeepEqual(got, []string{"busybox", "sh"}) {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestFindNoShell(t *testing.T) {
	p, _ := probeWith()
	if _, err := Find(context.Background(), p); !errors.Is(err, ErrNoShell) {
		t.Errorf("err = %v, want ErrNoShell", err)
	}
}

func TestFindHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, _ := probeWith("bash")
	if _, err := Find(ctx, p); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
