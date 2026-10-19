//go:build windows

package process

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func startTree(cmd *exec.Cmd) (processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return processTree{}, fmt.Errorf("create process job: %w", err)
	}
	closeJob := func() { _ = windows.CloseHandle(job) }
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		closeJob()
		return processTree{}, err
	}
	// Suspend before assigning the job: the child cannot spawn outside our job.
	// Go closes the initial thread handle, so reopen the suspended primary thread.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED}
	if err = cmd.Start(); err != nil {
		closeJob()
		return processTree{}, err
	}
	fail := func(err error) (processTree, error) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		closeJob()
		return processTree{}, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fail(err)
	}
	err = windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if err != nil {
		return fail(fmt.Errorf("assign process job: %w", err))
	}
	if err = resumePrimaryThread(uint32(cmd.Process.Pid)); err != nil {
		return fail(err)
	}
	return processTree{
		interrupt: func(os.Signal) {
			_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
		},
		kill:  func() { _ = windows.TerminateJobObject(job, 1) },
		close: closeJob,
		alive: func() bool {
			// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION from winnt.h.
			var accounting struct {
				TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
				TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
			}
			err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil)
			return err != nil || accounting.ActiveProcesses > 0
		},
	}, nil
}

func resumePrimaryThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		return err
	}
	return fmt.Errorf("cannot find suspended process thread: %w", err)
}
func exitCode(exit *exec.ExitError) int { return exit.ExitCode() }
func terminationSignals() []os.Signal   { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
func environmentKey(k string) string    { return strings.ToUpper(k) }
