// Copyright 2026 The gVisor Authors.
// SPDX-License-Identifier: Apache-2.0

package systrap

import (
	"unsafe"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/hostarch"
)

func (t *syscallThread) dxgArgs() ([]byte, uintptr) {
	return t.stubMessage.dxgArgs[:], t.stubAddr + syscallStubMessageOffset + unsafe.Offsetof(syscallStubMessage{}.dxgArgs)
}

// dxgIPCMessage constructs the native 64-bit msghdr/iovec/cmsghdr layout.
// Send metadata (especially the FD) is in the guest-read-only page. Receive
// metadata is necessarily writable, but no descriptor from it is trusted.
func (t *syscallThread) dxgIPCMessage(send bool, fd int) uintptr {
	b := t.sentryMessage.dxgIPC[:]
	addr := t.stubAddr + unsafe.Offsetof(syscallSentryMessage{}.dxgIPC)
	if !send {
		b = t.stubMessage.dxgIPC[:]
		addr = t.stubAddr + syscallStubMessageOffset + unsafe.Offsetof(syscallStubMessage{}.dxgIPC)
	}
	clear(b)
	u64 := func(offset int, value uint64) { hostarch.ByteOrder.PutUint64(b[offset:], value) }
	u32 := func(offset int, value uint32) { hostarch.ByteOrder.PutUint32(b[offset:], value) }
	u64(16, uint64(addr+56)) // iov
	u64(24, 1)               // iovlen
	u64(32, uint64(addr+72)) // control
	u64(40, 24)              // controllen
	u64(56, uint64(addr+96)) // iov_base
	u64(64, 1)               // iov_len
	if send {
		u64(72, 20) // CMSG_LEN(sizeof(int))
		u32(80, unix.SOL_SOCKET)
		u32(84, unix.SCM_RIGHTS)
		u32(88, uint32(fd))
		b[96] = 1
	}
	return addr
}
