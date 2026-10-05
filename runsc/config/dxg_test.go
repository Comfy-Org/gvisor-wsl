// Copyright 2026 The gVisor Authors.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"gvisor.dev/gvisor/runsc/flag"
)

func TestDXGProxyMode(t *testing.T) {
	for _, value := range []string{"disabled", "experimental"} {
		var mode DXGProxyMode
		if err := mode.Set(value); err != nil || mode.String() != value {
			t.Errorf("Set(%q) = %v (%v)", value, mode, err)
		}
	}
	for _, value := range []string{"", "true", "enabled", "secure", "auto"} {
		var mode DXGProxyMode
		if err := mode.Set(value); err == nil {
			t.Errorf("accepted unsupported mode %q", value)
		}
	}
}

func TestDXGProxyConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*Config)
		wantError bool
	}{
		{"supported", func(*Config) {}, false},
		{"kvm", func(c *Config) { c.Platform = "kvm" }, true},
		{"ptrace", func(c *Config) { c.Platform = "ptrace" }, true},
		{"without-directfs", func(c *Config) { c.DirectFS = false }, true},
		{"nvproxy", func(c *Config) { c.NVProxy = true }, true},
		{"nvproxy-docker", func(c *Config) { c.NVProxyDocker = true }, true},
		{"invalid-mode", func(c *Config) { c.DXGProxy = DXGProxyMode(123) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			RegisterFlags(fs)
			c, err := NewFromFlags(fs)
			if err != nil {
				t.Fatal(err)
			}
			if c.DXGProxy != DXGProxyDisabled {
				t.Fatal("DXG must default to disabled")
			}
			c.DXGProxy = DXGProxyExperimental
			c.Platform = "systrap"
			c.DirectFS = true
			tc.change(c)
			if err := c.Validate(); (err != nil) != tc.wantError {
				t.Errorf("Validate() = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}
