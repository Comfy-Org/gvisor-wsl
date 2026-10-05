// Copyright 2026 The gVisor Authors.
// SPDX-License-Identifier: Apache-2.0

package dxgproxy

import (
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/abi/dxg"
	"gvisor.dev/gvisor/pkg/abi/linux"
	"gvisor.dev/gvisor/pkg/context"
	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/sentry/kernel"
	"gvisor.dev/gvisor/pkg/sentry/platform"
	"gvisor.dev/gvisor/pkg/sentry/vfs"
)

// sharedFile is an opaque IPC capability, not a generally usable host file.
// In particular, it has no read, write, mmap or ioctl forwarding methods.
type sharedFile struct {
	vfsfd vfs.FileDescription
	vfs.FileDescriptionDefaultImpl
	vfs.DentryMetadataFileDescriptionImpl
	vfs.NoLockFD
	hostFD int
}

// ExperimentalDXGHostFD authorizes ONLY these opaque capabilities for outgoing
// host SCM_RIGHTS. Ordinary host-backed files retain the existing prohibition.
func (f *sharedFile) ExperimentalDXGHostFD() int { return f.hostFD }
func (f *sharedFile) Release(context.Context)    { unix.Close(f.hostFD) }

func newSharedFile(t *kernel.Task, fd int) (*vfs.FileDescription, error) {
	vd := t.Kernel().VFS().NewAnonVirtualDentry("[dxg-shared-resource]")
	defer vd.DecRef(t)
	f := &sharedFile{hostFD: fd}
	if err := f.vfsfd.Init(f, linux.O_RDWR, t.Credentials(), vd.Mount(), vd.Dentry(), &vfs.FileDescriptionOptions{UseDentryMetadata: true}); err != nil {
		return nil, err
	}
	return &f.vfsfd, nil
}

// ImportHostFD wraps an anonymous host IPC FD when DXG was explicitly enabled.
// The trusted peer donates the FD; dxgkrnl checks the actual resource f_ops on
// use. Anonymous non-DXG FDs cannot be used as DXG resources. This does not
// enable forwarding ioctls or file operations on arbitrary donated FDs.
// A successful non-nil return takes ownership of fd. Otherwise it is borrowed.
func ImportHostFD(ctx context.Context, fd int) (*vfs.FileDescription, error) {
	t := kernel.TaskFromContext(ctx)
	if t == nil {
		return nil, nil
	}
	as, ok := t.MemoryManager().AddressSpace().(platform.DXGAddressSpace)
	if !ok || !as.DXGEnabled() {
		return nil, nil
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != 0 {
		return nil, nil
	}
	return newSharedFile(t, fd)
}

func (f *fileDescription) ioctlFD(t *kernel.Task, request uint32, arg hostarch.Addr, size int) (uintptr, error) {
	params := make([]byte, size)
	if _, err := t.CopyInBytes(arg, params); err != nil {
		return 0, err
	}
	if request == dxg.ShareObjects {
		out := hostarch.Addr(hostarch.ByteOrder.Uint64(params[32:]))
		value, hostFD, err := f.file.IoctlFD(request, params, -1)
		if err != nil {
			return 0, err
		}
		file, err := newSharedFile(t, hostFD)
		if err != nil {
			unix.Close(hostFD)
			return 0, err
		}
		defer file.DecRef(t)
		fd, err := t.NewFDFrom(0, file, kernel.FDFlags{CloseOnExec: true})
		if err != nil {
			return 0, err
		}
		b := make([]byte, 8)
		hostarch.ByteOrder.PutUint64(b, uint64(fd))
		if _, err := t.CopyOutBytes(out, b); err != nil {
			if removed := t.FDTable().Remove(t, fd); removed != nil {
				removed.DecRef(t)
			}
			return 0, err
		}
		return value, nil
	}
	appFD := hostarch.ByteOrder.Uint64(params[8:])
	if appFD > 0x7fffffff {
		return 0, linuxerr.EBADF
	}
	file := t.GetFile(int32(appFD))
	if file == nil {
		return 0, linuxerr.EBADF
	}
	defer file.DecRef(t)
	shared, ok := file.Impl().(*sharedFile)
	if !ok {
		return 0, linuxerr.EINVAL
	}
	value, _, err := f.file.IoctlFD(request, params, shared.hostFD)
	if err != nil {
		return 0, err
	}
	hostarch.ByteOrder.PutUint64(params[8:], appFD)
	_, err = t.CopyOutBytes(arg, params)
	return value, err
}
