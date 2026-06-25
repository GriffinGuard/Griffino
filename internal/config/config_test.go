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

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsBindLocal(t *testing.T) {
	// File not found → defaults to localhost-only / 文件不存在，默认仅绑本机
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want 127.0.0.1", cfg.Server.ListenHost)
	}
	if cfg.Server.ListenPort != 7070 {
		t.Errorf("ListenPort = %d, want 7070", cfg.Server.ListenPort)
	}
}

func TestLoadServerOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listenHost: 0.0.0.0\n  listenPort: 8080\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenHost != "0.0.0.0" || cfg.Server.ListenPort != 8080 {
		t.Errorf("override not applied: %+v", cfg.Server)
	}
}

func TestLoadPartialServerKeepsDefaultPort(t *testing.T) {
	// Set host only, no port → port stays at default 7070 / 只设 host 不设 port，port 保留默认 7070
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  listenHost: localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ListenPort != 7070 {
		t.Errorf("ListenPort = %d, want default 7070", cfg.Server.ListenPort)
	}
}
