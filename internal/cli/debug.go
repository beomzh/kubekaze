package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// DefaultDebugImage is pinned by digest (busybox 1.37.0 multi-arch index).
// Override with --debug-image or KUBEKAZE_DEBUG_IMAGE, e.g. a netshoot image.
const DefaultDebugImage = "busybox:1.37.0@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"

// checkDebugImage refuses mutable references so a debug session cannot be
// silently swapped for different content.
func checkDebugImage(image string) error {
	if image == "" {
		return fmt.Errorf("empty debug image")
	}
	if strings.Contains(image, "@sha256:") {
		return nil
	}
	// strip registry host:port, keep the last path element for the tag check
	last := image[strings.LastIndex(image, "/")+1:]
	_, tag, ok := strings.Cut(last, ":")
	if !ok || tag == "" || tag == "latest" {
		return fmt.Errorf("debug image %q must use a fixed tag or digest, not latest", image)
	}
	return nil
}

func newDebugName() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "kubekaze-dbg-" + hex.EncodeToString(b), nil
}

// buildDebugContainer is deliberately unprivileged: no privilege escalation,
// all capabilities dropped, default seccomp profile.
func buildDebugContainer(name, image, targetContainer string) corev1.EphemeralContainer {
	f := false
	return corev1.EphemeralContainer{
		EphemeralContainerCommon: corev1.EphemeralContainerCommon{
			Name:            name,
			Image:           image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Stdin:           true,
			TTY:             true,
			SecurityContext: &corev1.SecurityContext{
				AllowPrivilegeEscalation: &f,
				Privileged:               &f,
				Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
		},
		TargetContainerName: targetContainer,
	}
}

// startDebugContainer adds an ephemeral container to the pod and waits until
// it runs. Ephemeral containers cannot be removed from a pod afterwards.
func startDebugContainer(ctx context.Context, cs kubernetes.Interface, ns, podName, target, image string) (string, error) {
	name, err := newDebugName()
	if err != nil {
		return "", err
	}
	pod, err := cs.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, buildDebugContainer(name, image, target))
	if _, err := cs.CoreV1().Pods(ns).UpdateEphemeralContainers(ctx, podName, pod, metav1.UpdateOptions{}); err != nil {
		return "", fmt.Errorf("adding ephemeral container (needs the pods/ephemeralcontainers permission and Kubernetes 1.25+): %w", err)
	}
	err = wait.PollUntilContextTimeout(ctx, time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
		p, err := cs.CoreV1().Pods(ns).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, st := range p.Status.EphemeralContainerStatuses {
			if st.Name != name {
				continue
			}
			if st.State.Running != nil {
				return true, nil
			}
			if w := st.State.Waiting; w != nil && (w.Reason == "ErrImagePull" || w.Reason == "ImagePullBackOff" || w.Reason == "InvalidImageName") {
				return false, fmt.Errorf("debug container cannot start: %s: %s", w.Reason, w.Message)
			}
			if st.State.Terminated != nil {
				return false, fmt.Errorf("debug container exited: %s", st.State.Terminated.Reason)
			}
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	return name, nil
}

func debugImageFrom(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("KUBEKAZE_DEBUG_IMAGE"); env != "" {
		return env
	}
	return DefaultDebugImage
}
