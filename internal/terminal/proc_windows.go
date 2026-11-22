//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// winProc is a shell on a ConPTY (the pseudo console of Windows 10 version 1809 and newer).
//
// The console host keeps the output pipe open after the shell has ended, so a reader would wait for ever: a goroutine waits for the
// process and then closes the pseudo console, which ends the output. The pipe handles are released by Wait, after the reader is done
// (closing a handle that a read is blocked on can hang).
type winProc struct {
	in      *os.File // what is typed
	out     *os.File // what the shell prints
	console windows.Handle
	process windows.Handle
	job     windows.Handle // kills the programs the shell started when it closes; 0 when it could not be made

	exited    chan struct{} // closed when the shell has ended and the pseudo console is closed
	mu        sync.Mutex    // protects console and the process handles against use after they were closed
	consoleOK bool
	released  bool // Wait has closed the process handles
	closeOnce sync.Once
}

// conPTYAvailable tells whether this Windows has the pseudo console functions (10 version 1809 and newer). The x/sys wrappers panic
// instead of failing when a function is missing, so this is checked before anything is created.
func conPTYAvailable() bool {
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	return kernel.NewProc("CreatePseudoConsole").Find() == nil && kernel.NewProc("ClosePseudoConsole").Find() == nil
}

// startProcess runs command in dir inside a ConPTY of width x height cells. The environment of jd is inherited as it is (command.Env and
// TERM are for unix terminals); the program is the one exec.LookPath found for command.
func startProcess(command *exec.Cmd, dir string, width, height int) (process, error) {
	if !conPTYAvailable() {
		return nil, ErrUnsupported
	}
	if command.Err != nil {
		return nil, command.Err
	}
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("create the input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("create the output pipe: %w", err)
	}
	closeAll := func() {
		for _, h := range []windows.Handle{inRead, inWrite, outRead, outWrite} {
			if h != 0 {
				windows.CloseHandle(h)
			}
		}
	}

	var console windows.Handle
	size := windows.Coord{X: int16(min(width, 32767)), Y: int16(min(height, 32767))}
	if err := windows.CreatePseudoConsole(size, inRead, outWrite, 0, &console); err != nil {
		closeAll()
		return nil, fmt.Errorf("create the pseudo console: %w", err)
	}
	// the console host has its own copies of its ends of the pipes now
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	inRead, outWrite = 0, 0

	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		closeAll() // the read end first: ClosePseudoConsole can wait for the output to be read
		windows.ClosePseudoConsole(console)
		return nil, fmt.Errorf("attribute list: %w", err)
	}
	defer attributes.Delete()
	// the attribute's value is the console handle itself, passed as a pointer
	handleValue := *(*unsafe.Pointer)(unsafe.Pointer(&console))
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, handleValue, unsafe.Sizeof(console)); err != nil {
		closeAll() // the read end first: ClosePseudoConsole can wait for the output to be read
		windows.ClosePseudoConsole(console)
		return nil, fmt.Errorf("attach the pseudo console: %w", err)
	}

	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(command.Args))
	var application *uint16
	if err == nil {
		application, err = windows.UTF16PtrFromString(command.Path) // not searched for again: no program of the working directory runs by accident
	}
	if err != nil {
		closeAll() // the read end first: ClosePseudoConsole can wait for the output to be read
		windows.ClosePseudoConsole(console)
		return nil, err
	}
	var workDir *uint16
	if dir != "" {
		if workDir, err = windows.UTF16PtrFromString(dir); err != nil {
			windows.ClosePseudoConsole(console)
			closeAll()
			return nil, err
		}
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attributes.List()}
	startup.StartupInfo.Cb = uint32(unsafe.Sizeof(startup))
	// With the standard handles named in the flags (all of them 0), the shell takes the pseudo console as its console. Without this a
	// shell started by a program whose own standard handles are redirected (a test run, a pipe) writes to those handles instead and
	// the screen stays empty.
	startup.StartupInfo.Flags = windows.STARTF_USESTDHANDLES
	var info windows.ProcessInformation
	// created suspended, so that it is in the job before it can start a program of its own
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(application, commandLine, nil, nil, false, flags, nil, workDir, &startup.StartupInfo, &info); err != nil {
		closeAll() // the read end first: ClosePseudoConsole can wait for the output to be read
		windows.ClosePseudoConsole(console)
		return nil, fmt.Errorf("start %s: %w", command.Path, err)
	}
	job := killOnCloseJob(info.Process) // 0 when it could not be made: closing then ends the shell only, not what it started
	_, resumeErr := windows.ResumeThread(info.Thread)
	windows.CloseHandle(info.Thread)
	if resumeErr != nil {
		_ = windows.TerminateProcess(info.Process, 1)
		windows.CloseHandle(info.Process)
		if job != 0 {
			windows.CloseHandle(job)
		}
		closeAll()
		windows.ClosePseudoConsole(console)
		return nil, fmt.Errorf("start %s: %w", command.Path, resumeErr)
	}

	p := &winProc{
		in: os.NewFile(uintptr(inWrite), "conpty-in"), out: os.NewFile(uintptr(outRead), "conpty-out"),
		console: console, process: info.Process, job: job,
		exited: make(chan struct{}), consoleOK: true,
	}
	go p.watch()
	return p, nil
}

// killOnCloseJob puts the process into a job object that ends everything in it when the job closes; 0 when that is not possible.
func killOnCloseJob(process windows.Handle) windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return 0
	}
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		windows.CloseHandle(job)
		return 0
	}
	return job
}

// watch waits for the shell, then closes the pseudo console so that the output ends.
func (p *winProc) watch() {
	_, _ = windows.WaitForSingleObject(p.process, windows.INFINITE)
	p.closeConsole()
	close(p.exited)
}

func (p *winProc) closeConsole() {
	p.mu.Lock()
	open := p.consoleOK
	p.consoleOK = false
	p.mu.Unlock()
	if open {
		windows.ClosePseudoConsole(p.console) // may wait until the output is read: Read goes on in its own goroutine, and Resize and Close are not held up
	}
}

func (p *winProc) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *winProc) Write(b []byte) (int, error) { return p.in.Write(b) }

func (p *winProc) Resize(width, height int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.consoleOK {
		return errors.New("the pseudo console is closed")
	}
	return windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(min(width, 32767)), Y: int16(min(height, 32767))})
}

// Wait returns once the shell has ended and the pseudo console is closed; it then releases the handles (the reader has finished).
func (p *winProc) Wait() error {
	<-p.exited
	_ = p.out.Close()
	_ = p.in.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.released = true
	windows.CloseHandle(p.process)
	if p.job != 0 {
		windows.CloseHandle(p.job)
	}
	return nil
}

// Close ends the shell and, through the job object, the programs it started. Wait does the rest when the shell is gone.
func (p *winProc) Close(<-chan struct{}) {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.released {
			return
		}
		if p.job != 0 {
			_ = windows.TerminateJobObject(p.job, 1)
		}
		_ = windows.TerminateProcess(p.process, 1)
	})
}
