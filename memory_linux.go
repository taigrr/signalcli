//go:build linux

package signalcli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// sampleRSS reads resident set size from /proc/<pid>/statm on Linux.
//
// statm reports sizes in pages; the second field is resident pages. We
// multiply by the system page size (4KiB on virtually all Linux systems) to
// get bytes. The ctx is accepted for API symmetry; the read is local and fast.
func sampleRSS(_ context.Context, pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
	if err != nil {
		return 0, fmt.Errorf("read statm for pid %d: %w", pid, err)
	}

	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected statm format for pid %d: %q", pid, string(data))
	}

	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse resident pages %q: %w", fields[1], err)
	}

	return pages * uint64(os.Getpagesize()), nil
}
