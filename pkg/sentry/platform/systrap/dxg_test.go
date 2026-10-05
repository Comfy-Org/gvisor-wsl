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

package systrap

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/abi/dxg"
	"gvisor.dev/gvisor/pkg/hostarch"
	"gvisor.dev/gvisor/pkg/sentry/arch"
	"gvisor.dev/gvisor/pkg/sentry/platform"
)

// Each mode needs a fresh process because systrap has a global stub pool.
func dxgTestAddressSpace(t *testing.T, enabled bool) platform.AddressSpace {
	t.Helper()
	if os.Getenv("GVISOR_DXG_HELPER_TEST") != t.Name() {
		cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v", "-test.timeout=20s")
		cmd.Env = append(os.Environ(), "GVISOR_DXG_HELPER_TEST="+t.Name())
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("helper subprocess: %v\n%s", err, output)
		}
		return nil
	}
	s, err := New(platform.Options{DisableSyscallPatching: true, DisableFastPath: true, ExperimentalDXG: enabled})
	if err != nil {
		t.Fatal(err)
	}
	as, err := s.NewAddressSpace()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(as.Release)
	return as
}

func TestDXGHelperFDIsolation(t *testing.T) {
	as := dxgTestAddressSpace(t, true)
	if as == nil {
		return
	}
	dir := t.TempDir()
	sentinel, err := os.Create(filepath.Join(dir, "dxg"))
	if err != nil {
		t.Fatal(err)
	}
	defer sentinel.Close()
	dirFD, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dirFD)

	f, err := as.(platform.DXGAddressSpace).OpenDXG(dirFD, unix.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	// A regular file rejects ioctls; this exercises the real ptrace request
	// and return path without requiring a WSL GPU on CI.
	_, ioctlErr := f.Ioctl(dxg.EnumAdapters, 0)
	// Exercise real SCM_RIGHTS import, helper FD translation, and temporary
	// descriptor cleanup repeatedly without a GPU. The regular-file device
	// rejects the final ioctl after the FD has crossed into the helper.
	for i := 0; i < 32; i++ {
		params := make([]byte, 40)
		_, exported, err := f.IoctlFD(dxg.QueryResourceInfo, params, int(sentinel.Fd()))
		if !errors.Is(err, unix.ENOTTY) || exported != -1 {
			t.Fatalf("FD import ioctl %d = %d, %v; want -1, ENOTTY", i, exported, err)
		}
	}
	f.Close()
	if !errors.Is(ioctlErr, unix.ENOTTY) {
		t.Errorf("regular-file ioctl: got %v, want ENOTTY", ioctlErr)
	}
	// close_range and close in the helper must not affect the Sentry table.
	if _, err := sentinel.WriteString("still open"); err != nil {
		t.Errorf("helper damaged the caller's FD table: %v", err)
	}
	var st unix.Stat_t
	if err := unix.Fstat(dirFD, &st); err != nil {
		t.Errorf("helper closed borrowed directory in caller: %v", err)
	}
}

func TestDXGHelperRejectsSymlinks(t *testing.T) {
	as := dxgTestAddressSpace(t, true)
	if as == nil {
		return
	}
	dir := t.TempDir()
	if err := os.Symlink("/dev/null", filepath.Join(dir, "dxg")); err != nil {
		t.Fatal(err)
	}
	dirFD, err := unix.Open(dir, unix.O_PATH|unix.O_DIRECTORY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dirFD)
	f, err := as.(platform.DXGAddressSpace).OpenDXG(dirFD, unix.O_RDWR)
	if f != nil {
		f.Close()
	}
	if !errors.Is(err, unix.ELOOP) {
		t.Errorf("symlink device: got %v, want ELOOP", err)
	}
}

func TestDXGHelperDisabled(t *testing.T) {
	as := dxgTestAddressSpace(t, false)
	if as == nil {
		return
	}
	if f, err := as.(platform.DXGAddressSpace).OpenDXG(0, unix.O_RDWR); f != nil || !errors.Is(err, unix.EOPNOTSUPP) {
		t.Fatalf("disabled OpenDXG = %v, %v; want EOPNOTSUPP", f, err)
	}
}

func TestDXGHostMappingsStayBelowStub(t *testing.T) {
	as := dxgTestAddressSpace(t, true)
	if as == nil {
		return
	}
	s := as.(*subprocess)
	// Force hints into each excluded interval, including the random gap
	// between stub code and stacks. Without guards Linux honors these hints,
	// reproducing the host-ASLR-dependent DXG fence registration failure.
	for _, hint := range []uintptr{maxStubUserAddress, stubStart - hostarch.PageSize, stubROMapEnd, maximumUserAddress - hostarch.PageSize} {
		addr, err := s.syscall(unix.SYS_MMAP,
			arch.SyscallArgument{Value: hint},
			arch.SyscallArgument{Value: hostarch.PageSize},
			arch.SyscallArgument{Value: unix.PROT_READ | unix.PROT_WRITE},
			arch.SyscallArgument{Value: unix.MAP_PRIVATE | unix.MAP_ANONYMOUS},
			arch.SyscallArgument{Value: ^uintptr(0)},
			arch.SyscallArgument{Value: 0})
		if err != nil {
			t.Fatal(err)
		}
		if addr >= maxStubUserAddress {
			t.Errorf("host mmap hint %#x returned %#x above application limit %#x", hint, addr, maxStubUserAddress)
		}
		s.Unmap(hostarch.Addr(addr), hostarch.PageSize)
	}
}
