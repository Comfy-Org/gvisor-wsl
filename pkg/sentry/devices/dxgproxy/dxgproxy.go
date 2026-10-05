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

// Package dxgproxy provides experimental WSL GPU compatibility.
//
// dxgkrnl binds opens to a host tgid and installs CPU mappings in current->mm.
// Consequently it cannot use nvproxy's Sentry-side ioctl forwarding. Instead
// an isolated platform helper opens the device and executes modeled requests
// in the application's address space.
//
// This package is a compatibility experiment, not a security-reviewed proxy.
// Checkpoint/restore and sharing an open DXG file across fork/exec are unsupported.
package dxgproxy

import (
	"gvisor.dev/gvisor/pkg/abi/dxg"
	"gvisor.dev/gvisor/pkg/abi/linux"
	"gvisor.dev/gvisor/pkg/context"
	"gvisor.dev/gvisor/pkg/devutil"
	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/log"
	"gvisor.dev/gvisor/pkg/sentry/arch"
	"gvisor.dev/gvisor/pkg/sentry/kernel"
	"gvisor.dev/gvisor/pkg/sentry/kernel/auth"
	"gvisor.dev/gvisor/pkg/sentry/mm"
	"gvisor.dev/gvisor/pkg/sentry/platform"
	"gvisor.dev/gvisor/pkg/sentry/vfs"
	"gvisor.dev/gvisor/pkg/sync"
	"gvisor.dev/gvisor/pkg/usermem"
)

type device struct{}

// Register registers the OCI-authorized misc device minor. WSL allocates the
// minor dynamically; it must not be hard-coded to one host's value.
func Register(vfsObj *vfs.VirtualFilesystem, minor uint32) error {
	return vfsObj.RegisterDevice(vfs.CharDevice, 10, minor, &device{}, &vfs.RegisterDeviceOptions{
		GroupName: "dxg", Pathname: "dxg", FilePerms: 0666,
	})
}

// Open implements vfs.Device.Open.
func (*device) Open(ctx context.Context, mnt *vfs.Mount, d *vfs.Dentry, opts vfs.OpenOptions) (*vfs.FileDescription, error) {
	t := kernel.TaskFromContext(ctx)
	client := devutil.GoferClientFromContext(ctx)
	if t == nil || client == nil || client.HostDirFD() < 0 {
		return nil, linuxerr.ENODEV
	}
	owner := t.MemoryManager()
	as, ok := owner.AddressSpace().(platform.DXGAddressSpace)
	if !ok {
		return nil, linuxerr.EOPNOTSUPP
	}
	if !owner.IncUsers() {
		return nil, linuxerr.ESRCH
	}
	file, err := as.OpenDXG(client.HostDirFD(), opts.Flags&linux.O_ACCMODE)
	if err != nil {
		owner.DecUsers(ctx)
		log.Warningf("dxgproxy: opening helper device: %v", err)
		return nil, err
	}
	f := &fileDescription{owner: owner, file: file, mappings: owner.NewPlatformMappingOwner()}
	if err := f.vfsfd.Init(f, opts.Flags, auth.CredentialsFromContext(ctx), mnt, d, &vfs.FileDescriptionOptions{UseDentryMetadata: true}); err != nil {
		file.Close()
		owner.DecUsers(ctx)
		return nil, err
	}
	return &f.vfsfd, nil
}

type fileDescription struct {
	vfsfd vfs.FileDescription
	vfs.FileDescriptionDefaultImpl
	vfs.DentryMetadataFileDescriptionImpl
	vfs.NoLockFD

	owner       *mm.MemoryManager
	file        platform.DXGFile
	mu          sync.Mutex
	pins        []mm.PinnedRange
	pinnedBytes uint64
	mappings    *mm.PlatformMappingOwner
	broken      bool
}

// Release implements vfs.FileDescriptionImpl.Release.
func (f *fileDescription) Release(ctx context.Context) {
	if !f.broken {
		f.file.Close()
	}
	f.mappings.Release(ctx)
	mm.Unpin(f.pins)
	f.owner.DecUsers(ctx)
}

// Ioctl implements vfs.FileDescriptionImpl.Ioctl.
func (f *fileDescription) Ioctl(ctx context.Context, uio usermem.IO, sysno uintptr, args arch.SyscallArguments) (uintptr, error) {
	t := kernel.TaskFromContext(ctx)
	if t == nil || t.MemoryManager() != f.owner {
		return 0, linuxerr.EXDEV
	}
	request := args[1].Uint()
	if !dxg.Supported(request) {
		log.Warningf("dxgproxy: unsupported ioctl %#x", request)
		return 0, linuxerr.ENOTTY
	}
	arg := args[2].Pointer()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		return 0, linuxerr.ENODEV
	}
	size := uint64((request >> 16) & 0x3fff)
	ar, ok := arg.ToRange(size)
	if !ok {
		return 0, linuxerr.EFAULT
	}
	// Host-kernel copies cannot fault pages into the guest MM. Materialize
	// both the top-level argument and the modeled nested buffers first.
	if err := f.owner.EnsurePlatformMapping(ctx, ar, hostarch.ReadWrite); err != nil {
		return 0, err
	}
	if err := f.prepareBuffers(t, request, arg, int(size)); err != nil {
		log.Debugf("dxgproxy: preparing ioctl %#x: %v", request, err)
		return 0, err
	}
	if request == dxg.ShareObjects || request == dxg.QueryResourceInfo || request == dxg.OpenResource {
		return f.ioctlFD(t, request, arg, int(size))
	}
	result, err := f.ioctlWithMappings(t, request, ar)
	log.Debugf("dxgproxy: ioctl %#x -> %#x, %v", request, result, err)
	return result, err
}
