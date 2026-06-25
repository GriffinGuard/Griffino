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

package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/GriffinGuard/Griffino/internal/imagecheck"
)

func TestFindRegistryPlugin(t *testing.T) {
	reg := &Registry{Plugins: []RegistryPlugin{
		{ID: "a"}, {ID: "b"},
	}}
	if p := findRegistryPlugin(reg, "b"); p == nil || p.ID != "b" {
		t.Fatalf("expected to find b, got %v", p)
	}
	if p := findRegistryPlugin(reg, "missing"); p != nil {
		t.Fatalf("expected nil for missing, got %v", p)
	}
}

func TestHasVersion(t *testing.T) {
	p := &RegistryPlugin{
		LatestVersion: "1.2.0",
		Versions: []RegistryVersion{
			{Version: "1.0.0"}, {Version: "1.1.0"}, {Version: "1.2.0"},
		},
	}
	for _, v := range []string{"1.2.0", "1.1.0", "1.0.0"} {
		if !hasVersion(p, v) {
			t.Fatalf("expected hasVersion(%q) = true", v)
		}
	}
	if hasVersion(p, "9.9.9") {
		t.Fatal("expected hasVersion(9.9.9) = false")
	}
}

func TestRegistryBaseURL(t *testing.T) {
	// Defaults to official source (locked) / 默认锁死到官方源
	t.Setenv("GRIFFINO_REGISTRY_BASE_URL", "")
	if got := registryBaseURL(); got != defaultRegistryBaseURL {
		t.Fatalf("default base = %q, want %q", got, defaultRegistryBaseURL)
	}
	if got := registryIndexURL(); got != defaultRegistryBaseURL+"/registry.json" {
		t.Fatalf("default index = %q", got)
	}
	// Override via environment variable, trimming trailing slash / 环境变量覆盖，且去掉尾部斜杠
	t.Setenv("GRIFFINO_REGISTRY_BASE_URL", "http://localhost:9999/reg/")
	if got := registryBaseURL(); got != "http://localhost:9999/reg" {
		t.Fatalf("override base = %q, want trimmed", got)
	}
	if got := registryIndexURL(); got != "http://localhost:9999/reg/registry.json" {
		t.Fatalf("override index = %q", got)
	}
}

func TestRegistryFetchErrStatus(t *testing.T) {
	// Unapproved image -> 403 / 镜像未批准
	unapproved := &imagecheck.UnapprovedError{Images: []string{"evil:latest"}}
	if status, code := registryFetchErrStatus(unapproved); status != http.StatusForbidden || code != ErrPluginImageNotAllowed {
		t.Fatalf("unapproved -> (%d,%s), want (403,%s)", status, code, ErrPluginImageNotAllowed)
	}
	// Other errors -> 502 / 其它错误
	if status, code := registryFetchErrStatus(errors.New("boom")); status != http.StatusBadGateway || code != ErrRegistryDownloadFailed {
		t.Fatalf("generic -> (%d,%s), want (502,%s)", status, code, ErrRegistryDownloadFailed)
	}
}
