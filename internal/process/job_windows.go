//go:build windows

package process

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// KillChildrenOnExit puts the current process into a job object that kills
// every child when the last handle to the job closes, that is when this
// process ends in any way (including "hydravpn-router stop" on Windows, which
// has to kill it). Without it sing-box would be left running.
func KillChildrenOnExit() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return err
	}
	// The handle stays open for the life of the process on purpose.
	return windows.AssignProcessToJobObject(job, windows.CurrentProcess())
}
