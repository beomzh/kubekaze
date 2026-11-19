package kube

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// CheckTimeout bounds the connectivity check.
const CheckTimeout = 3 * time.Second

// Check asks the API server for its version. A server rejects invalid
// credentials even on this public endpoint, so it detects expired logins as
// well as unreachable clusters. The returned error is written for humans.
func Check(ctx context.Context, cfg *rest.Config) error {
	c := rest.CopyConfig(cfg)
	c.Timeout = CheckTimeout
	cs, err := kubernetes.NewForConfig(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout+time.Second)
	defer cancel()
	_, err = cs.Discovery().RESTClient().Get().AbsPath("/version").DoRaw(ctx)
	return explain(err)
}

func explain(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case apierrors.IsUnauthorized(err):
		return errors.New("the API server rejected the credentials (Unauthorized): the login or token for this context has probably expired; log in again (oc login, aws eks update-kubeconfig, az aks get-credentials, gcloud container clusters get-credentials, ...)")
	case apierrors.IsForbidden(err):
		return nil // reachable and authenticated; /version is simply not allowed
	}
	var nerr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &nerr) && nerr.Timeout()) {
		return fmt.Errorf("the API server did not answer within %s: check your network or VPN", CheckTimeout)
	}
	return fmt.Errorf("cannot reach the API server: %w", err)
}
