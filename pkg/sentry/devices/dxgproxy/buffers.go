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
	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/log"
	"gvisor.dev/gvisor/pkg/sentry/kernel"
	"gvisor.dev/gvisor/pkg/sentry/mm"
)

// These are experiment resource limits, not a claim that the opaque NVIDIA
// escape/private-data ABI has been validated. In particular the host driver
// determines non-standard allocation lengths after communicating with Windows.
const (
	maxPrivateBytes    = 128 << 10
	maxArrayElements   = 1024
	maxPinnedBytes     = 512 << 20
	allocationInfoSize = 96
)

// prepareBuffers materializes the known UAPI pointer fields. Reads are bounded;
// GPU virtual addresses are deliberately not treated as CPU pointers.
//
// This function does not eliminate TOCTOU with other guest threads. The feature
// must remain gated as experimental until immutable argument marshalling and
// allocation-size validation are implemented.
func (f *fileDescription) prepareBuffers(t *kernel.Task, request uint32, arg hostarch.Addr, size int) error {
	b := make([]byte, size)
	if _, err := t.CopyInBytes(arg, b); err != nil {
		return err
	}
	u32 := func(off int) uint64 { return uint64(hostarch.ByteOrder.Uint32(b[off:])) }
	u64 := func(off int) uint64 { return hostarch.ByteOrder.Uint64(b[off:]) }
	buffer := func(addr, length uint64, at hostarch.AccessType) error {
		if length == 0 {
			return nil
		}
		if length > maxPrivateBytes {
			return linuxerr.E2BIG
		}
		ar, ok := hostarch.Addr(addr).ToRange(length)
		if !ok || addr == 0 {
			return linuxerr.EFAULT
		}
		return f.owner.EnsurePlatformMapping(t, ar, at)
	}
	array := func(addr, count, itemSize uint64, at hostarch.AccessType) error {
		if count > maxArrayElements {
			return linuxerr.E2BIG
		}
		return buffer(addr, count*itemSize, at)
	}
	switch request {
	case dxg.ShareObjects:
		// The kernel currently accepts exactly one shared object per call.
		if u32(0) != 1 {
			return linuxerr.EINVAL
		}
		if err := buffer(u64(8), 4, hostarch.Read); err != nil {
			return err
		}
		return buffer(u64(32), 8, hostarch.Write)
	case dxg.OpenResource:
		if err := array(u64(24), u32(16), 80, hostarch.ReadWrite); err != nil {
			return err
		}
		for _, offsets := range [][2]int{{40, 32}, {56, 48}, {72, 64}, {88, 96}} {
			if err := buffer(u64(offsets[0]), u32(offsets[1]), hostarch.ReadWrite); err != nil {
				return err
			}
		}
	case dxg.QueryVideoMemoryInfo:
		// The WSL kernel only supports the calling process (process == 0).
		// All remaining fields are scalar inputs/outputs; no nested buffers.
		if u64(0) != 0 {
			return linuxerr.EINVAL
		}
	case dxg.EnumAdapters:
		// A NULL adapter list is the count-only query.
		if u64(16) != 0 {
			return array(u64(16), u32(8), 20, hostarch.ReadWrite)
		}
	case dxg.QueryAdapterInfo:
		return buffer(u64(8), u32(16), hostarch.ReadWrite)
	case dxg.Escape:
		return buffer(u64(16), u32(24), hostarch.ReadWrite)
	case dxg.CreateContext:
		return buffer(u64(16), u32(24), hostarch.ReadWrite)
	case dxg.CreateHWQueue:
		return buffer(u64(16), u32(8), hostarch.ReadWrite)
	case dxg.CreateAllocation:
		if err := buffer(u64(16), u32(24), hostarch.Read); err != nil {
			return err
		}
		privateSize := u32(40)
		if u32(56)&(1<<16) != 0 {
			privateSize = 24 // d3dkmt_createstandardallocation
		}
		if err := buffer(u64(32), privateSize, hostarch.Read); err != nil {
			return err
		}
		count := u32(44)
		if err := array(u64(48), count, allocationInfoSize, hostarch.ReadWrite); err != nil {
			return err
		}
		infos := make([]byte, int(count)*allocationInfoSize)
		if _, err := t.CopyInBytes(hostarch.Addr(u64(48)), infos); err != nil {
			return err
		}
		for i := 0; i < len(infos); i += allocationInfoSize {
			info := infos[i:]
			private := hostarch.ByteOrder.Uint64(info[16:])
			privateLen := uint64(hostarch.ByteOrder.Uint32(info[24:]))
			if err := buffer(private, privateLen, hostarch.ReadWrite); err != nil {
				return err
			}
			if sysmem := hostarch.ByteOrder.Uint64(info[8:]); sysmem != 0 {
				ar, err := f.owner.PlatformBufferRange(hostarch.Addr(sysmem), maxPinnedBytes-f.pinnedBytes)
				if err != nil {
					return err
				}
				pins, err := f.owner.Pin(t, ar, hostarch.ReadWrite, false)
				if err != nil {
					mm.Unpin(pins)
					return err
				}
				// Conservatively retain these references until the DXG file
				// closes (even on ioctl failure), after the driver unpins them.
				f.pins = append(f.pins, pins...)
				f.pinnedBytes += uint64(ar.Length())
				log.Debugf("dxgproxy: pinning allocation VMA %v (%d bytes total)", ar, f.pinnedBytes)
				if err := f.owner.EnsurePlatformMapping(t, ar, hostarch.ReadWrite); err != nil {
					return err
				}
			}
		}
	case dxg.MakeResident:
		if err := array(u64(8), u32(4), 4, hostarch.Read); err != nil {
			return err
		}
		if u64(16) != 0 {
			return array(u64(16), u32(4), 4, hostarch.Read)
		}
	case dxg.DestroyAllocation:
		return array(u64(8), u32(16), 4, hostarch.Read)
	case dxg.SubmitCommand:
		if err := buffer(u64(32), u32(28), hostarch.Read); err != nil {
			return err
		}
		return array(u64(48), u32(40), 4, hostarch.Read)
	case dxg.SubmitWait:
		if err := array(u64(8), u32(4), 4, hostarch.Read); err != nil {
			return err
		}
		return array(u64(16), u32(4), 8, hostarch.Read)
	}
	return nil
}
