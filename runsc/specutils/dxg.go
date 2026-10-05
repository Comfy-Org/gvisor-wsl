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
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"gvisor.dev/gvisor/runsc/config"
)

// DXGEnabled requires both explicit runtime opt-in and an OCI DXG device.
// Merely running under WSL never grants a sandbox GPU access.
func DXGEnabled(spec *specs.Spec, conf *config.Config) bool {
	if conf.DXGProxy != config.DXGProxyExperimental || spec.Linux == nil {
		return false
	}
	for _, dev := range spec.Linux.Devices {
		if dev.Path == "/dev/dxg" && (dev.Type == "c" || dev.Type == "u") && dev.Major == 10 && dev.Minor > 0 && dev.Minor <= 0xfffff {
			return true
		}
	}
	return false
}
