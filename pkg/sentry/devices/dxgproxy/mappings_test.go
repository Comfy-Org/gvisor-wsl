// Copyright 2026 The gVisor Authors.
// SPDX-License-Identifier: Apache-2.0

package dxgproxy

import (
	"testing"

	"gvisor.dev/gvisor/pkg/abi/dxg"
	"gvisor.dev/gvisor/pkg/hostarch"
)

func TestFenceRange(t *testing.T) {
	for _, tc := range []struct {
		request uint32
		kind    uint32
		offset  int
	}{
		{dxg.CreatePagingQueue, 0, 16},
		{dxg.CreateHWQueue, 0, 32},
		{dxg.CreateSyncObject, 5, 24},
		{dxg.CreateSyncObject, 6, 32},
	} {
		b := make([]byte, (tc.request>>16)&0x3fff)
		hostarch.ByteOrder.PutUint32(b[8:], tc.kind)
		hostarch.ByteOrder.PutUint64(b[tc.offset:], 0x1234080)
		ar, err := fenceRange(tc.request, b)
		if err != nil || ar.Start != 0x1234000 || ar.Length() != hostarch.PageSize {
			t.Errorf("fence %#x kind %d = %v, %v", tc.request, tc.kind, ar, err)
		}
		hostarch.ByteOrder.PutUint64(b[tc.offset:], 0)
		if _, err := fenceRange(tc.request, b); err == nil {
			t.Errorf("accepted NULL fence for %#x", tc.request)
		}
		if _, err := fenceRange(tc.request, b[:tc.offset]); err == nil {
			t.Errorf("accepted truncated fence for %#x", tc.request)
		}
	}
	if ar, err := fenceRange(dxg.CreateSyncObject, make([]byte, 96)); err != nil || ar.Length() != 0 {
		t.Errorf("non-fence sync object = %v, %v", ar, err)
	}
}
