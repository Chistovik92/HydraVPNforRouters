//go:build windows

package selfupdate

import (
	"os"

	"golang.org/x/sys/windows"
)

// replace swaps the binaries. Windows cannot overwrite a running executable
// but can rename it, so the old file is moved aside to "<bin>.old" and
// removed at the next start (CleanupOld).
func replace(bin, newBin string) error {
	old := bin + ".old"
	os.Remove(old)
	if err := os.Rename(bin, old); err != nil {
		return err
	}
	if err := os.Rename(newBin, bin); err != nil {
		os.Rename(old, bin) // put the working binary back
		return err
	}
	return nil
}

func freeSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return 0, err
	}
	return free, nil
}

// CleanupOld removes the binary left behind by the previous update.
func CleanupOld(bin string) {
	if bin == "" {
		if exe, err := os.Executable(); err == nil {
			bin = exe
		}
	}
	if bin != "" {
		os.Remove(bin + ".old")
	}
}
