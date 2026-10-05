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

package specutils

import (
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"gvisor.dev/gvisor/runsc/config"
)

func TestDXGRequiresExplicitDeviceAndOptIn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		dev     specs.LinuxDevice
		want    bool
	}{
		{"enabled", true, specs.LinuxDevice{Path: "/dev/dxg", Type: "c", Major: 10, Minor: 258}, true},
		{"different-dynamic-minor", true, specs.LinuxDevice{Path: "/dev/dxg", Type: "c", Major: 10, Minor: 257}, true},
		{"disabled", false, specs.LinuxDevice{Path: "/dev/dxg", Type: "c", Major: 10, Minor: 258}, false},
		{"prefix", true, specs.LinuxDevice{Path: "/dev/dxg-extra", Type: "c", Major: 10, Minor: 258}, false},
		{"block", true, specs.LinuxDevice{Path: "/dev/dxg", Type: "b", Major: 10, Minor: 258}, false},
		{"wrong-major", true, specs.LinuxDevice{Path: "/dev/dxg", Type: "c", Major: 1, Minor: 258}, false},
		{"negative-minor", true, specs.LinuxDevice{Path: "/dev/dxg", Type: "c", Major: 10, Minor: -1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := &specs.Spec{Linux: &specs.Linux{Devices: []specs.LinuxDevice{tc.dev}}}
			mode := config.DXGProxyDisabled
			if tc.enabled {
				mode = config.DXGProxyExperimental
			}
			if got := DXGEnabled(spec, &config.Config{DXGProxy: mode}); got != tc.want {
				t.Fatalf("DXGEnabled = %v, want %v", got, tc.want)
			}
		})
	}
	if DXGEnabled(&specs.Spec{}, &config.Config{DXGProxy: config.DXGProxyExperimental}) {
		t.Error("enabled without an OCI Linux device list")
	}
}
