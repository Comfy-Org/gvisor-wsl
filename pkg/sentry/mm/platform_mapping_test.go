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

package mm

import (
	"testing"

	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/safemem"
	"gvisor.dev/gvisor/pkg/sentry/contexttest"
	"gvisor.dev/gvisor/pkg/sentry/memmap"
	"gvisor.dev/gvisor/pkg/sentry/platform"
)

type recordingAddressSpace struct {
	platform.AddressSpace
	unmaps []hostarch.AddrRange
}

func (as *recordingAddressSpace) Unmap(addr hostarch.Addr, length uint64) {
	as.unmaps = append(as.unmaps, hostarch.AddrRange{Start: addr, End: addr + hostarch.Addr(length)})
}

func TestPlatformMappingPreservedAndReleased(t *testing.T) {
	ctx := contexttest.Context(t)
	mm := testMemoryManager(ctx, t)
	defer mm.DecUsers(ctx)
	arg, err := mm.MMap(ctx, memmap.MMapOpts{
		Length: hostarch.PageSize, Private: true, Perms: hostarch.ReadWrite, MaxPerms: hostarch.ReadWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	owner := mm.NewPlatformMappingOwner()
	ar := hostarch.AddrRange{Start: arg + 4*hostarch.PageSize, End: arg + 5*hostarch.PageSize}
	create := func(ims safemem.BlockSeq) (hostarch.AddrRange, error) {
		if ims.NumBytes() != 32 {
			t.Errorf("argument size = %d, want 32", ims.NumBytes())
		}
		return ar, nil
	}
	argRange := hostarch.AddrRange{Start: arg + 8, End: arg + 40}
	if err := owner.Create(ctx, argRange, create); err != nil {
		t.Fatal(err)
	}
	// Reusing a device-owned address must not create duplicate reservations.
	if err := owner.Create(ctx, argRange, create); err != nil {
		t.Fatal(err)
	}
	if got := len(mm.platformMappings); got != 1 {
		t.Fatalf("mapping count = %d, want 1", got)
	}
	if err := mm.MUnmap(ctx, ar.Start, uint64(ar.Length())); err != linuxerr.EBUSY {
		t.Errorf("MUnmap = %v, want EBUSY", err)
	}
	if _, err := mm.MMap(ctx, memmap.MMapOpts{
		Addr: ar.Start, Length: hostarch.PageSize, Fixed: true, Unmap: true,
		Perms: hostarch.ReadWrite, MaxPerms: hostarch.ReadWrite,
	}); err != linuxerr.EBUSY {
		t.Errorf("MAP_FIXED = %v, want EBUSY", err)
	}
	if err := mm.MProtect(ar.Start, hostarch.PageSize, hostarch.NoAccess, false); err != linuxerr.EBUSY {
		t.Errorf("MProtect = %v, want EBUSY", err)
	}
	if err := mm.Decommit(ar.Start, hostarch.PageSize); err != linuxerr.EBUSY {
		t.Errorf("Decommit = %v, want EBUSY", err)
	}
	if err := mm.SetDontFork(ar.Start, hostarch.PageSize, false); err != linuxerr.EBUSY {
		t.Errorf("MADV_DOFORK = %v, want EBUSY", err)
	}
	if _, err := mm.MRemap(ctx, ar.Start, hostarch.PageSize, hostarch.PageSize, MRemapOpts{Move: MRemapMustMove, NewAddr: ar.End}); err != linuxerr.EBUSY {
		t.Errorf("MRemap = %v, want EBUSY", err)
	}
	as := &recordingAddressSpace{AddressSpace: mm.as}
	mm.as = as
	mm.activeMu.Lock()
	mm.unmapASLocked(hostarch.AddrRange{Start: ar.Start - hostarch.PageSize, End: ar.End + hostarch.PageSize})
	mm.activeMu.Unlock()
	if len(as.unmaps) != 2 || as.unmaps[0].End != ar.Start || as.unmaps[1].Start != ar.End {
		t.Fatalf("unmaps = %v, want two ranges excluding %v", as.unmaps, ar)
	}
	owner.Release(ctx)
	if len(mm.platformMappings) != 0 || mm.vmas.FindSegment(ar.Start).Ok() {
		t.Fatal("release left a reservation behind")
	}
	if got := as.unmaps[len(as.unmaps)-1]; got != ar {
		t.Errorf("release unmap = %v, want %v", got, ar)
	}
}

func TestPlatformMappingRejectsCollision(t *testing.T) {
	ctx := contexttest.Context(t)
	mm := testMemoryManager(ctx, t)
	defer mm.DecUsers(ctx)
	arg, err := mm.MMap(ctx, memmap.MMapOpts{
		Length: hostarch.PageSize, Private: true, Perms: hostarch.ReadWrite, MaxPerms: hostarch.ReadWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	ar := hostarch.AddrRange{Start: arg, End: arg + hostarch.PageSize}
	err = mm.NewPlatformMappingOwner().Create(ctx, hostarch.AddrRange{Start: arg, End: arg + 32}, func(safemem.BlockSeq) (hostarch.AddrRange, error) { return ar, nil })
	if err != linuxerr.EEXIST {
		t.Errorf("colliding mapping = %v, want EEXIST", err)
	}
}

func TestPlatformBufferRangeBounds(t *testing.T) {
	ctx := contexttest.Context(t)
	mm := testMemoryManager(ctx, t)
	defer mm.DecUsers(ctx)
	arg, err := mm.MMap(ctx, memmap.MMapOpts{
		Length: 2 * hostarch.PageSize, Private: true, Perms: hostarch.ReadWrite, MaxPerms: hostarch.ReadWrite,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mm.PlatformBufferRange(arg, hostarch.PageSize); err != linuxerr.ENOMEM {
		t.Errorf("over-budget range = %v, want ENOMEM", err)
	}
	if _, err := mm.PlatformBufferRange(arg+1, 2*hostarch.PageSize); err != linuxerr.EINVAL {
		t.Errorf("unaligned range = %v, want EINVAL", err)
	}
	if _, err := mm.PlatformBufferRange(arg+2*hostarch.PageSize, hostarch.PageSize); err != linuxerr.EFAULT {
		t.Errorf("unmapped range = %v, want EFAULT", err)
	}
}
