//go:build !windows

package selfupdate

import (
	"os"

	"golang.org/x/sys/unix"
)

// replace moves the verified new binary over the running one. rename(2) is
// atomic and the running process keeps its open copy of the old file.
func replace(bin, newBin string) error {
	if err := os.Rename(newBin, bin); err != nil {
		return err
	}
	unix.Sync()
	return nil
}

func freeSpace(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// CleanupOld is a no-op outside Windows (see the Windows version).
func CleanupOld(bin string) {}
