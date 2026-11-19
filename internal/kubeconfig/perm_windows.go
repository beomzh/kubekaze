//go:build windows

package kubeconfig

import "os"

func runtimeUnixPerm(os.FileInfo) int { return 0 }
