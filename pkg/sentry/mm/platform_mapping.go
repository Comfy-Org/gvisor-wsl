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
	"sort"

	"gvisor.dev/gvisor/pkg/context"
	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/safemem"
	"gvisor.dev/gvisor/pkg/sentry/memmap"
)

// PlatformMappingOwner tracks mappings installed by an experimental device
// directly in the platform address space, without a backing memmap.File. The
// owner must release its device before calling Release, and retain a user
// reference to mm until then. Mappings remain reserved until owner release,
// even if the device destroys/reuses individual mappings earlier.
//
// These mappings cannot be copied, moved, checkpointed or accessed through
// usermem.IO. Guest attempts to change their permissions or replace them fail.
type PlatformMappingOwner struct {
	mm *MemoryManager
}

// NewPlatformMappingOwner creates a mapping owner for mm.
func (mm *MemoryManager) NewPlatformMappingOwner() *PlatformMappingOwner {
	return &PlatformMappingOwner{mm: mm}
}

// Create serializes a device operation and registration of its new mapping
// against VMA changes and AS invalidations. io receives the internally-mapped
// top-level argument buffer, and returns the range installed by the device (or
// an empty range if none). io must not call back into mm.
//
// This protects mapping metadata, not ioctl arguments against guest writes.
// On registration failure the caller must close the device before continuing;
// a host-selected address can collide with a lazily-mapped guest VMA.
func (o *PlatformMappingOwner) Create(ctx context.Context, arg hostarch.AddrRange, io func(safemem.BlockSeq) (hostarch.AddrRange, error)) error {
	mm := o.mm
	if !arg.WellFormed() || arg.Length() == 0 {
		return linuxerr.EFAULT
	}
	mm.mappingMu.Lock()
	defer mm.mappingMu.Unlock()
	vseg, _, err := mm.getVMAsLocked(ctx, arg, hostarch.ReadWrite, false)
	if err != nil {
		return err
	}
	mm.activeMu.Lock()
	defer mm.activeMu.Unlock()
	pseg, _, err := mm.getPMAsLocked(ctx, vseg, arg, hostarch.ReadWrite, true, false)
	if err != nil {
		return err
	}
	end, ok := arg.End.RoundUp()
	if !ok {
		return linuxerr.EFAULT
	}
	if err := mm.mapASLocked(ctx, pseg, hostarch.AddrRange{Start: arg.Start.RoundDown(), End: end}, memmap.PlatformEffectPopulate); err != nil {
		return err
	}
	ims, tx, err := mm.getIOMappingsLocked(pseg, arg, hostarch.ReadWrite)
	if err != nil || ims.NumBytes() != uint64(arg.Length()) {
		tx.flush(0, nil)
		return linuxerr.EFAULT
	}
	ar, err := io(ims)
	if _, flushErr := tx.flush(0, nil); flushErr != nil {
		return flushErr
	}
	if err != nil || ar.Length() == 0 {
		return err
	}
	if !ar.WellFormed() || !ar.IsPageAligned() || !mm.applicationAddrRange().IsSupersetOf(ar) {
		ctx.Warningf("platform device mapping %v is outside application range %v", ar, mm.applicationAddrRange())
		return linuxerr.EFAULT
	}
	for _, existing := range mm.platformMappings {
		if existing.owner == o && existing.ar == ar {
			// dxgkrnl reuses addresses after destroying individual objects.
			return nil
		}
	}
	if !mm.vmas.IsEmptyRange(ar) {
		return linuxerr.EEXIST
	}
	m := &platformMapping{owner: o, ar: ar}
	vseg, _, _, err = mm.createVMALocked(ctx, memmap.MMapOpts{
		Addr: ar.Start, Length: uint64(ar.Length()), Fixed: true, NoReplace: true,
		Perms: hostarch.ReadWrite, MaxPerms: hostarch.ReadWrite,
		Mappable: m, Name: "[platform-device]",
	}, nil)
	if err != nil {
		return err
	}
	vseg.ValuePtr().dontfork = true
	mm.platformMappings = append(mm.platformMappings, m)
	sort.Slice(mm.platformMappings, func(i, j int) bool { return mm.platformMappings[i].ar.Start < mm.platformMappings[j].ar.Start })
	return nil
}

// Release removes this owner's reservations after the device has been closed.
func (o *PlatformMappingOwner) Release(ctx context.Context) {
	mm := o.mm
	mm.mappingMu.Lock()
	defer mm.mappingMu.Unlock()
	mm.activeMu.Lock()
	defer mm.activeMu.Unlock()
	retained := mm.platformMappings[:0]
	for _, m := range mm.platformMappings {
		if m.owner != o {
			retained = append(retained, m)
			continue
		}
		// These VMAs have no PMAs or MappingIdentity references.
		mm.removeVMAsLocked(ctx, m.ar, nil)
		mm.as.Unmap(m.ar.Start, uint64(m.ar.Length()))
	}
	clear(mm.platformMappings[len(retained):])
	mm.platformMappings = retained
}

// Preconditions: mm.mappingMu or mm.activeMu must be held.
func (mm *MemoryManager) overlapsPlatformMappingLocked(ar hostarch.AddrRange) bool {
	for _, m := range mm.platformMappings {
		if m.ar.Overlaps(ar) {
			return true
		}
	}
	return false
}

type platformMapping struct {
	owner *PlatformMappingOwner
	ar    hostarch.AddrRange
}

func (*platformMapping) AddMapping(context.Context, memmap.MappingSpace, hostarch.AddrRange, uint64, bool) error {
	return nil
}

func (*platformMapping) RemoveMapping(context.Context, memmap.MappingSpace, hostarch.AddrRange, uint64, bool) {
}

func (*platformMapping) CopyMapping(context.Context, memmap.MappingSpace, hostarch.AddrRange, hostarch.AddrRange, uint64, bool) error {
	return linuxerr.EOPNOTSUPP
}

func (*platformMapping) Translate(context.Context, memmap.MappableRange, memmap.MappableRange, hostarch.AccessType) ([]memmap.Translation, error) {
	// Never replace an unrecognized/missing device PTE with anonymous memory.
	return nil, &memmap.BusError{Err: linuxerr.EFAULT}
}

func (*platformMapping) InvalidateUnsavable(context.Context) error {
	return linuxerr.EOPNOTSUPP
}
