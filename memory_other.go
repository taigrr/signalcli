//go:build !linux && !darwin

package signalcli

import (
	"context"
	"fmt"
	"runtime"
)

// sampleRSS is unsupported outside Linux and macOS (e.g. Windows). Watch
// therefore cannot bound memory on these platforms.
func sampleRSS(_ context.Context, _ int) (uint64, error) {
	return 0, fmt.Errorf("memory sampling is not supported on %s", runtime.GOOS)
}
