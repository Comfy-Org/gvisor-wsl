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

package dxgproxy

import (
	"gvisor.dev/gvisor/pkg/abi/dxg"
	"gvisor.dev/gvisor/pkg/context"
	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/log"
	"gvisor.dev/gvisor/pkg/safemem"
)

func (f *fileDescription) ioctlWithMappings(ctx context.Context, request uint32, arg hostarch.AddrRange) (uintptr, error) {
	switch request {
	case dxg.CreatePagingQueue, dxg.CreateHWQueue, dxg.CreateSyncObject:
	default:
		return f.file.Ioctl(request, arg.Start)
	}
	var result uintptr
	var ioctlErr error
	err := f.mappings.Create(ctx, arg, func(ims safemem.BlockSeq) (hostarch.AddrRange, error) {
		result, ioctlErr = f.file.Ioctl(request, arg.Start)
		if ioctlErr != nil {
			return hostarch.AddrRange{}, ioctlErr
		}
		b := make([]byte, int(arg.Length()))
		if _, err := safemem.CopySeq(safemem.BlockSeqOf(safemem.BlockFromSafeSlice(b)), ims); err != nil {
			return hostarch.AddrRange{}, err
		}
		return fenceRange(request, b)
	})
	if err != nil && ioctlErr == nil {
		// The host operation succeeded, but its mapping could not be tracked.
		// Do not leave a usable file with untracked pages behind.
		log.Warningf("dxgproxy: cannot track fence mapping: %v; disabling device", err)
		f.file.Close()
		f.mappings.Release(ctx)
		f.broken = true
	}
	return result, err
}

func fenceRange(request uint32, b []byte) (hostarch.AddrRange, error) {
	var offset int
	switch request {
	case dxg.CreatePagingQueue:
		offset = 16
	case dxg.CreateHWQueue:
		offset = 32
	case dxg.CreateSyncObject:
		if len(b) < 12 {
			return hostarch.AddrRange{}, linuxerr.EINVAL
		}
		switch hostarch.ByteOrder.Uint32(b[8:]) {
		case 5: // monitored fence
			offset = 24
		case 6: // periodic monitored fence
			offset = 32
		default:
			return hostarch.AddrRange{}, nil
		}
	default:
		return hostarch.AddrRange{}, linuxerr.EINVAL
	}
	if len(b) < offset+8 {
		return hostarch.AddrRange{}, linuxerr.EINVAL
	}
	addr := hostarch.Addr(hostarch.ByteOrder.Uint64(b[offset:]))
	log.Debugf("dxgproxy: ioctl %#x returned fence address %#x", request, addr)
	if addr == 0 {
		return hostarch.AddrRange{}, linuxerr.EFAULT
	}
	// dxg_map_iospace maps one page. A monitored fence may be returned at an
	// offset within that page.
	ar, ok := addr.RoundDown().ToRange(hostarch.PageSize)
	if !ok {
		return hostarch.AddrRange{}, linuxerr.EFAULT
	}
	return ar, nil
}
