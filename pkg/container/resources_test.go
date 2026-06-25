// Copyright 2025 GriffinGuard
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

package container

import (
	"testing"

	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

func TestResolveResourcesDefaults(t *testing.T) {
	r := resolveResources(nil)
	if r.Memory != int64(defaultMemoryMB)*1024*1024 {
		t.Errorf("Memory = %d, want %d", r.Memory, int64(defaultMemoryMB)*1024*1024)
	}
	if r.NanoCPUs != int64(defaultCPUs*1e9) {
		t.Errorf("NanoCPUs = %d, want %d", r.NanoCPUs, int64(defaultCPUs*1e9))
	}
	if r.PidsLimit == nil || *r.PidsLimit != int64(defaultPidsLimit) {
		t.Errorf("PidsLimit = %v, want %d", r.PidsLimit, defaultPidsLimit)
	}
}

func TestResolveResourcesOverride(t *testing.T) {
	r := resolveResources(&manifest.ResourceLimits{MemoryMB: 256, CPUs: 0.5, PidsLimit: 100})
	if r.Memory != 256*1024*1024 {
		t.Errorf("Memory = %d, want %d", r.Memory, 256*1024*1024)
	}
	if r.NanoCPUs != 500_000_000 {
		t.Errorf("NanoCPUs = %d, want 500000000", r.NanoCPUs)
	}
	if r.PidsLimit == nil || *r.PidsLimit != 100 {
		t.Errorf("PidsLimit = %v, want 100", r.PidsLimit)
	}
}

func TestResolveResourcesPartialOverrideFallsBack(t *testing.T) {
	// Only memory is set; CPU and PIDs must fall back to the platform defaults.
	r := resolveResources(&manifest.ResourceLimits{MemoryMB: 1024})
	if r.Memory != 1024*1024*1024 {
		t.Errorf("Memory = %d, want %d", r.Memory, 1024*1024*1024)
	}
	if r.NanoCPUs != int64(defaultCPUs*1e9) {
		t.Errorf("NanoCPUs = %d, want default %d", r.NanoCPUs, int64(defaultCPUs*1e9))
	}
	if r.PidsLimit == nil || *r.PidsLimit != int64(defaultPidsLimit) {
		t.Errorf("PidsLimit = %v, want default %d", r.PidsLimit, defaultPidsLimit)
	}
}
