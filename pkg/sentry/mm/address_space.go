// Copyright 2018 The gVisor Authors.
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
	"fmt"

	"gvisor.dev/gvisor/pkg/context"
	"gvisor.dev/gvisor/pkg/errors/linuxerr"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/sentry/memmap"
	"gvisor.dev/gvisor/pkg/sentry/pgalloc"
	"gvisor.dev/gvisor/pkg/sentry/platform"
)

// AddressSpace returns the platform.AddressSpace bound to mm.
//
// Preconditions: mm.users != 0.
func (mm *MemoryManager) AddressSpace() platform.AddressSpace {
	return mm.as
}

// EnsurePlatformMapping validates ar and installs its pages in the platform AS.
// This is needed before a process-bound device ioctl accesses application
// memory: a host-kernel copy cannot invoke the Sentry's page-fault handler.
func (mm *MemoryManager) EnsurePlatformMapping(ctx context.Context, ar hostarch.AddrRange, at hostarch.AccessType) error {
	if ar.Length() == 0 {
		return nil
	}
	end, ok := ar.End.RoundUp()
	if !ok {
		return linuxerr.EFAULT
	}
	ar.Start, ar.End = ar.Start.RoundDown(), end
	mm.mappingMu.RLock()
	defer mm.mappingMu.RUnlock()
	vseg, _, err := mm.getVMAsLocked(ctx, ar, at, false)
	if err != nil {
		return err
	}
	mm.activeMu.Lock()
	defer mm.activeMu.Unlock()
	pseg, _, err := mm.getPMAsLocked(ctx, vseg, ar, at, true, false)
	if err != nil {
		return err
	}
	return mm.mapASLocked(ctx, pseg, ar, memmap.PlatformEffectPopulate)
}

// PlatformBufferRange returns the suffix of the readable/writable VMA containing
// addr. Some device ABIs encode allocation sizes in opaque, driver-specific
// data. An experimental caller can conservatively pin this entire range rather
// than guessing that encoding. This is not a security check on a later ioctl:
// the application can change its ioctl arguments or mappings after this call.
func (mm *MemoryManager) PlatformBufferRange(addr hostarch.Addr, limit uint64) (hostarch.AddrRange, error) {
	if !addr.IsPageAligned() {
		return hostarch.AddrRange{}, linuxerr.EINVAL
	}
	mm.mappingMu.RLock()
	defer mm.mappingMu.RUnlock()
	vseg := mm.vmas.FindSegment(addr)
	if !vseg.Ok() || !vseg.ValuePtr().effectivePerms.SupersetOf(hostarch.ReadWrite) {
		return hostarch.AddrRange{}, linuxerr.EFAULT
	}
	ar := hostarch.AddrRange{Start: addr, End: vseg.End()}
	if uint64(ar.Length()) > limit {
		return hostarch.AddrRange{}, linuxerr.ENOMEM
	}
	return ar, nil
}

// mapASLocked maps addresses in ar into mm.as.
//
// Preconditions:
//   - mm.activeMu must be locked.
//   - ar.Length() != 0.
//   - ar must be page-aligned.
//   - pseg == mm.pmas.LowerBoundSegment(ar.Start).
func (mm *MemoryManager) mapASLocked(ctx context.Context, pseg pmaIterator, ar hostarch.AddrRange, platformEffect memmap.MMapPlatformEffect) error {
	if platformEffect == memmap.PlatformEffectCommit && ar.Length() > (1<<30) {
		// FIXME(b/445932215, b/445939339): Don't precommit very large ranges
		// via platform.AddressSpace.MapFile() for now, since holding locks
		// while doing so causes problems.
		platformEffect = memmap.PlatformEffectPopulate
	}
	// By default, map entire pmas at a time, under the assumption that there
	// is no cost to mapping more of a pma than necessary.
	mapAR := hostarch.AddrRange{0, ^hostarch.Addr(hostarch.PageSize - 1)}
	setMapUnit := func(mapUnit uint64) {
		mapMask := hostarch.Addr(mapUnit - 1)
		mapAR.Start = ar.Start &^ mapMask
		// If rounding ar.End up overflows, just keep the existing mapAR.End.
		if end := (ar.End + mapMask) &^ mapMask; end >= ar.End {
			mapAR.End = end
		}
	}
	if platformEffect != memmap.PlatformEffectDefault {
		// When explicitly committing, only map ar, since overmapping may incur
		// unexpected resource usage. When explicitly populating, do the same
		// since an underlying device file may be sensitive to the mapped
		// range.
		mapAR = ar
	} else if mapUnit := mm.p.MapUnit(); mapUnit != 0 {
		// Limit the range we map to ar, aligned to mapUnit.
		setMapUnit(mapUnit)
	} else if mf, ok := pseg.ValuePtr().file.(*pgalloc.MemoryFile); ok && mf.IsAsyncLoading() {
		// Impose an arbitrary mapUnit in order to avoid calling
		// platform.AddressSpace.MapFile() => mf.DataFD() or mf.MapInternal()
		// with unnecessarily large ranges, resulting in unnecessarily long
		// waits.
		setMapUnit(32 << 20)
	}
	if checkInvariants {
		if !mapAR.IsSupersetOf(ar) {
			panic(fmt.Sprintf("mapAR %#v is not a superset of ar %#v", mapAR, ar))
		}
	}

	// Since this checks ar.End and not mapAR.End, we will never map a pma that
	// is not required.
	for pseg.Ok() && pseg.Start() < ar.End {
		pma := pseg.ValuePtr()
		pmaAR := pseg.Range()
		pmaMapAR := pmaAR.Intersect(mapAR)
		perms := pma.effectivePerms
		if pma.needCOW {
			perms.Write = false
		}
		if perms.Any() { // MapFile precondition
			// If the length of the mapping exceeds singleMapThreshold, call
			// AddressSpace.MapFile() on singleMapThreshold-aligned chunks so
			// we can check ctx.Killed() reasonably frequently.
			const singleMapThreshold = 1 << 30
			if pmaMapAR.Length() <= singleMapThreshold {
				if err := mm.as.MapFile(pmaMapAR.Start, pma.file, pseg.fileRangeOf(pmaMapAR), perms, platformEffect == memmap.PlatformEffectCommit); err != nil {
					return err
				}
				if ctx.Killed() {
					return linuxerr.EINTR
				}
			} else {
				for windowStart := pmaMapAR.Start &^ (singleMapThreshold - 1); windowStart < pmaMapAR.End; windowStart += singleMapThreshold {
					windowAR := hostarch.AddrRange{windowStart, windowStart + singleMapThreshold}
					thisMapAR := pmaMapAR.Intersect(windowAR)
					if err := mm.as.MapFile(thisMapAR.Start, pma.file, pseg.fileRangeOf(thisMapAR), perms, platformEffect == memmap.PlatformEffectCommit); err != nil {
						return err
					}
					if ctx.Killed() {
						return linuxerr.EINTR
					}
				}
			}
		}
		pseg = pseg.NextSegment()
	}
	return nil
}

// unmapASLocked removes all AddressSpace mappings for addresses in ar.
//
// Preconditions: mm.activeMu must be locked.
func (mm *MemoryManager) unmapASLocked(ar hostarch.AddrRange) {
	if ar.Length() == 0 {
		return
	}
	if mm.as == nil {
		// Being called from mm.DecUsers() after AddressSpace release.
		return
	}
	// unmapASLocked doesn't require vmas or pmas to exist for ar, so it can be
	// passed ranges that include addresses that can't be mapped by the
	// application.
	ar = ar.Intersect(mm.applicationAddrRange())
	// Normal invalidations deliberately over-unmap gaps between PMAs. Device
	// PFN mappings have no PMAs and cannot be recreated by MapFile; preserve
	// them until their owner explicitly releases them.
	for _, m := range mm.platformMappings {
		if !m.ar.Overlaps(ar) {
			continue
		}
		if ar.Start < m.ar.Start {
			mm.as.Unmap(ar.Start, uint64(m.ar.Start-ar.Start))
		}
		ar.Start = m.ar.End
		if ar.Start >= ar.End {
			return
		}
	}
	if ar.Length() != 0 {
		mm.as.Unmap(ar.Start, uint64(ar.Length()))
	}
}
