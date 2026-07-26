//go:build darwin

package signalcli

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// sampleRSS shells out to `ps -o rss= -p <pid>` on macOS, which reports the
// resident set size in kilobytes.
func sampleRSS(ctx context.Context, pid int) (uint64, error) {
	out, err := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("sample memory for pid %d: %w", pid, err)
	}

	field := strings.TrimSpace(string(out))
	if field == "" {
		return 0, fmt.Errorf("no memory info for pid %d (process gone?)", pid)
	}

	kb, err := strconv.ParseUint(field, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse rss %q: %w", field, err)
	}

	return kb * 1024, nil
}
