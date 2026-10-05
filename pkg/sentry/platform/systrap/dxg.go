// Copyright 2026 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package systrap

import (
	"runtime"
	"sort"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/abi/dxg"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/seccomp"
	"gvisor.dev/gvisor/pkg/sentry/arch"
	"gvisor.dev/gvisor/pkg/sentry/platform"
)

const dxgCloneFlags = uintptr(unix.CLONE_VM | unix.CLONE_FS | unix.CLONE_PTRACE | unix.CLONE_PARENT | unix.SIGCHLD)

type dxgRequest struct {
	request uint32
	arg     hostarch.Addr
	params  []byte
	hostFD  int
	done    chan dxgResult
}

type dxgResult struct {
	value uintptr
	fd    int
	err   error
}

// dxgFile has an isolated host FD table, so no DXG ioctl can consume a Sentry
// FD number supplied by an application. The helper is always ptrace-stopped
// between calls; return registers are read by the Sentry, not from guest-
// writable memory. It shares only the application's address space.
type dxgFile struct {
	requests chan dxgRequest
	closed   chan struct{}
}

// DXGEnabled implements platform.DXGAddressSpace.
func (*subprocess) DXGEnabled() bool { return dxgEnabled }

// OpenDXG implements platform.DXGAddressSpace.
func (s *subprocess) OpenDXG(dirFD int, flags uint32) (platform.DXGFile, error) {
	if !dxgEnabled {
		return nil, unix.EOPNOTSUPP
	}
	if dirFD < 0 || flags & ^uint32(unix.O_ACCMODE) != 0 {
		return nil, unix.EINVAL
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(pair[1])
	req := requestThread{thread: make(chan *thread, 1), dxg: true}
	s.requests <- req
	t := <-req.thread
	if t == nil {
		unix.Close(pair[0])
		return nil, errDeadSubprocess
	}
	f := &dxgFile{requests: make(chan dxgRequest), closed: make(chan struct{})}
	ready := make(chan error, 1)
	go func() { // S/R-SAFE: Platform-related. DXG does not support checkpointing.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(f.closed)
		defer unix.Close(pair[0])
		if err := t.attach(); err != nil {
			t.kill()
			ready <- err
			return
		}
		defer t.destroy()
		message := syscallThread{subproc: s, thread: t}
		if err := message.init(false); err != nil {
			ready <- err
			return
		}
		defer message.destroy()
		call := func(sysno uintptr, values ...uintptr) (uintptr, error) {
			args := make([]arch.SyscallArgument, len(values))
			for i, value := range values {
				args[i].Value = value
			}
			return t.syscallIgnoreInterrupt(&t.initRegs, sysno, args...)
		}
		// The clone copied the FD table. Prune it before processing any
		// application request, retaining the device directory and private IPC
		// endpoint. Never retain the Sentry's other endpoint in this table.
		keep := []int{dirFD, pair[1]}
		sort.Ints(keep)
		start := uintptr(0)
		for _, next := range append(keep, int(0x100000000)) {
			if start < uintptr(next) {
				if _, err := call(unix.SYS_CLOSE_RANGE, start, uintptr(next-1), 0); err != nil {
					ready <- err
					return
				}
			}
			start = uintptr(next + 1)
		}
		fd, err := call(unix.SYS_OPENAT, uintptr(dirFD), message.dxgPathAddr(), uintptr(flags)|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			ready <- err
			return
		}
		if _, err := call(unix.SYS_CLOSE, uintptr(dirFD)); err != nil {
			ready <- err
			return
		}
		ready <- nil
		// Only the device and socket remain. All temporary imports/exports are
		// closed after each operation, so Linux must allocate this lowest slot.
		// Do NOT trust descriptor numbers in guest-writable syscall replies.
		slot := uintptr(0)
		for slot == fd || slot == uintptr(pair[1]) {
			slot++
		}
		for request := range f.requests {
			if request.params != nil {
				result, exported, err := dxgFDCall(&message, call, fd, uintptr(pair[1]), pair[0], slot, request)
				request.done <- dxgResult{result, exported, err}
			} else {
				result, err := call(unix.SYS_IOCTL, fd, uintptr(request.request), uintptr(request.arg))
				request.done <- dxgResult{result, -1, err}
			}
		}
		// dxgkrnl may unmap fence pages on release: close in the same mm.
		call(unix.SYS_CLOSE, fd)
	}()
	if err := <-ready; err != nil {
		<-f.closed
		return nil, err
	}
	return f, nil
}

// Ioctl implements platform.DXGFile.
func (f *dxgFile) Ioctl(request uint32, arg hostarch.Addr) (uintptr, error) {
	if !dxg.Supported(request) || request == dxg.ShareObjects || request == dxg.QueryResourceInfo || request == dxg.OpenResource {
		return 0, unix.ENOTTY
	}
	done := make(chan dxgResult, 1)
	f.requests <- dxgRequest{request: request, arg: arg, done: done}
	result := <-done
	return result.value, result.err
}

// IoctlFD implements platform.DXGFile.
func (f *dxgFile) IoctlFD(request uint32, params []byte, hostFD int) (uintptr, int, error) {
	if (request != dxg.ShareObjects && request != dxg.QueryResourceInfo && request != dxg.OpenResource) || len(params) != int(request>>16&0x3fff) {
		return 0, -1, unix.ENOTTY
	}
	done := make(chan dxgResult, 1)
	f.requests <- dxgRequest{request: request, params: params, hostFD: hostFD, done: done}
	result := <-done
	return result.value, result.fd, result.err
}

func dxgFDCall(message *syscallThread, call func(uintptr, ...uintptr) (uintptr, error), device, socket uintptr, sentrySocket int, slot uintptr, request dxgRequest) (uintptr, int, error) {
	args, addr := message.dxgArgs()
	copy(args, request.params)
	if request.request != dxg.ShareObjects {
		if request.hostFD < 0 {
			return 0, -1, unix.EBADF
		}
		if _, err := unix.SendmsgN(sentrySocket, []byte{1}, unix.UnixRights(request.hostFD), nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL); err != nil {
			return 0, -1, err
		}
		if _, err := call(unix.SYS_RECVMSG, socket, message.dxgIPCMessage(false, 0), unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC); err != nil {
			return 0, -1, err
		}
		if _, err := call(unix.SYS_FCNTL, slot, unix.F_GETFD); err != nil {
			return 0, -1, err
		}
		defer call(unix.SYS_CLOSE, slot)
		hostarch.ByteOrder.PutUint64(args[8:], uint64(slot))
	} else {
		// ShareObjects writes its new FD to this helper-local scratch word.
		hostarch.ByteOrder.PutUint64(args[32:], uint64(addr+120))
	}
	value, err := call(unix.SYS_IOCTL, device, uintptr(request.request), addr)
	copy(request.params, args)
	if err != nil || request.request != dxg.ShareObjects {
		return value, -1, err
	}
	defer call(unix.SYS_CLOSE, slot)
	if _, err := call(unix.SYS_SENDMSG, socket, message.dxgIPCMessage(true, int(slot)), unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL); err != nil {
		return 0, -1, err
	}
	data, control := make([]byte, 1), make([]byte, unix.CmsgSpace(4))
	n, nc, flags, _, err := unix.Recvmsg(sentrySocket, data, control, unix.MSG_DONTWAIT|unix.MSG_TRUNC)
	if err != nil {
		return 0, -1, err
	}
	msgs, err := unix.ParseSocketControlMessage(control[:nc])
	if err != nil {
		return 0, -1, err
	}
	var fds []int
	for _, msg := range msgs {
		rights, err := unix.ParseUnixRights(&msg)
		if err == nil {
			fds = append(fds, rights...)
		}
	}
	if n != 1 || flags&unix.MSG_CTRUNC != 0 || len(fds) != 1 {
		for _, fd := range fds {
			unix.Close(fd)
		}
		return 0, -1, unix.EIO
	}
	return value, fds[0], nil
}

// Close implements platform.DXGFile. Calls must be serialized by the owner.
func (f *dxgFile) Close() {
	close(f.requests)
	<-f.closed
}

func dxgIoctlRules() seccomp.SyscallRule {
	var rules seccomp.Or
	for _, request := range dxg.Commands() {
		rules = append(rules, seccomp.PerArg{seccomp.NonNegativeFD{}, seccomp.EqualTo(uintptr(request))})
	}
	return rules
}
