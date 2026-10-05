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

// Package dxg describes the supported subset of the WSL dxgkrnl UAPI, from
// include/uapi/misc/d3dkmthk.h in Microsoft's WSL2-Linux-Kernel repository.
package dxg

// Full ioctl numbers include the direction, structure size, type and command.
// Checking only the command byte would accidentally accept incompatible ABIs.
const (
	OpenAdapterFromLUID  uint32 = 0xc00c4701
	CreateDevice         uint32 = 0xc0404702
	CreateContext        uint32 = 0xc0284704
	DestroyContext       uint32 = 0xc0044705
	CreateAllocation     uint32 = 0xc0484706
	CreatePagingQueue    uint32 = 0xc0204707
	ReserveGPUVA         uint32 = 0xc0484708
	QueryAdapterInfo     uint32 = 0xc0184709
	QueryVideoMemoryInfo uint32 = 0xc038470a
	MakeResident         uint32 = 0xc030470b
	MapGPUVA             uint32 = 0xc068470c
	Escape               uint32 = 0xc020470d
	CreateSyncObject     uint32 = 0xc0604710
	DestroyAllocation    uint32 = 0xc0184713
	CloseAdapter         uint32 = 0xc0044715
	CreateHWQueue        uint32 = 0xc0304718
	DestroyDevice        uint32 = 0xc0044719
	DestroyHWQueue       uint32 = 0xc004471b
	DestroyPagingQueue   uint32 = 0xc004471c
	DestroySyncObject    uint32 = 0xc004471d
	FreeGPUVA            uint32 = 0xc0184720
	SubmitCommand        uint32 = 0xc0384734
	SubmitWait           uint32 = 0xc0184736
	EnumAdapters         uint32 = 0xc018473e
	ShareObjects         uint32 = 0xc028473f
	QueryResourceInfo    uint32 = 0xc0284741
	OpenResource         uint32 = 0xc0684742
)

// Commands returns the exact request numbers supported by the experimental
// proxy. Resource FD import/export is supported; sync-file operations are not.
func Commands() []uint32 {
	return []uint32{
		OpenAdapterFromLUID, CreateDevice, CreateContext, DestroyContext,
		CreateAllocation, CreatePagingQueue, ReserveGPUVA, QueryAdapterInfo,
		QueryVideoMemoryInfo, MakeResident, MapGPUVA, Escape, CreateSyncObject, DestroyAllocation,
		CloseAdapter, CreateHWQueue, DestroyDevice, DestroyHWQueue,
		DestroyPagingQueue, DestroySyncObject, FreeGPUVA, SubmitCommand,
		SubmitWait, EnumAdapters, ShareObjects, QueryResourceInfo, OpenResource,
	}
}

// Supported reports whether request is in the explicit ABI allowlist.
func Supported(request uint32) bool {
	for _, command := range Commands() {
		if request == command {
			return true
		}
	}
	return false
}
