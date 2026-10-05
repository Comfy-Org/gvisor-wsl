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

package dxg

import "testing"

func TestQueryVideoMemoryInfo(t *testing.T) {
	// d3dkmt_queryvideomemoryinfo is a 56-byte flat structure, not a set
	// of user pointers. PyTorch's allocator requires this budget query.
	if QueryVideoMemoryInfo != 0xc038470a || !Supported(QueryVideoMemoryInfo) {
		t.Fatal("WSL video-memory budget query is not supported")
	}
}

func TestResourceSharingABI(t *testing.T) {
	for _, request := range []uint32{0xc028473f, 0xc0284741, 0xc0684742} {
		if !Supported(request) {
			t.Errorf("resource IPC ioctl %#x not supported", request)
		}
	}
	// Fence/sync-file sharing is intentionally outside the resource-FD path.
	if Supported(0xc0584740) {
		t.Error("unmodeled shared fence import enabled")
	}
}

func TestRejectIncompatibleRequests(t *testing.T) {
	for _, command := range Commands() {
		for _, bit := range []uint32{1 << 8, 1 << 16, 1 << 30, 1 << 31} {
			if Supported(command ^ bit) {
				t.Errorf("accepted changed type, size or direction: %#x", command^bit)
			}
		}
	}
	for _, request := range []uint32{0, 0xffffffff, 0xc020473f, 0xc0184745, 0xc0204746, 0xc0184747} {
		if Supported(request) {
			t.Errorf("accepted unmodeled or FD-bearing command %#x", request)
		}
	}
}
