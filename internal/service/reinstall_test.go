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

package service

import (
	"context"
	"path/filepath"
	"testing"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

func newReinstallTestService(t *testing.T) (*PluginService, *store.Store) {
	t.Helper()
	_ = griffinoi18n.Init("en") // so i18n.T() in service code doesn't nil-panic / 初始化 i18n，避免 T() 空指针
	st, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// Only the store is exercised by the non-running reinstall paths / 非运行态覆盖路径只用到 store
	return NewPluginService(nil, st, nil, nil), st
}

func bootConfig(params ...manifest.ConfigParam) *manifest.PluginPackage {
	return &manifest.PluginPackage{
		BootConfig: &manifest.BootConfig{
			Services: []manifest.ServiceConfig{{ID: "main", Configs: params}},
		},
	}
}

func TestReinstallDevPlugin_PreservesConfigWhenCompatible(t *testing.T) {
	svc, st := newReinstallTestService(t)

	existing := &store.PluginInstance{
		ID:          "demo",
		PluginDir:   "/old/dir",
		Status:      store.StatusReady,
		IsDevPlugin: true,
		AdminConfig: map[string]map[string]string{"main": {"token": "secret"}},
	}
	if err := st.SavePlugin(existing); err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}

	// New manifest has no missing required fields -> config compatible / 新 manifest 无缺失必填项 -> 配置兼容
	pkg := bootConfig(manifest.ConfigParam{Key: "token", Type: manifest.ConfigTypeString})
	got, err := svc.ReinstallDevPlugin(context.Background(), pkg, "/new/dir", existing)
	if err != nil {
		t.Fatalf("ReinstallDevPlugin: %v", err)
	}

	if got.Status != store.StatusReady {
		t.Errorf("status = %q, want ready", got.Status)
	}
	if got.PluginDir != "/new/dir" {
		t.Errorf("pluginDir = %q, want /new/dir", got.PluginDir)
	}
	if got.AdminConfig["main"]["token"] != "secret" {
		t.Errorf("AdminConfig not preserved: %v", got.AdminConfig)
	}

	// Persisted to the store / 已落库
	saved, _ := st.GetPlugin("demo")
	if saved == nil || saved.PluginDir != "/new/dir" || saved.Status != store.StatusReady {
		t.Errorf("persisted instance mismatch: %+v", saved)
	}
}

func TestReinstallDevPlugin_ResetsToPendingWhenRequiredFieldMissing(t *testing.T) {
	svc, st := newReinstallTestService(t)

	existing := &store.PluginInstance{
		ID:          "demo",
		PluginDir:   "/old/dir",
		Status:      store.StatusReady,
		IsDevPlugin: true,
		AdminConfig: map[string]map[string]string{"main": {"token": "secret"}},
	}
	if err := st.SavePlugin(existing); err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}

	// New manifest adds a required field the saved config lacks -> incompatible / 新 manifest 增加已保存配置缺失的必填项 -> 不兼容
	pkg := bootConfig(
		manifest.ConfigParam{Key: "token", Type: manifest.ConfigTypeString},
		manifest.ConfigParam{Key: "endpoint", Type: manifest.ConfigTypeString},
	)
	got, err := svc.ReinstallDevPlugin(context.Background(), pkg, "/new/dir", existing)
	if err != nil {
		t.Fatalf("ReinstallDevPlugin: %v", err)
	}

	if got.Status != store.StatusPendingSetup {
		t.Errorf("status = %q, want pending_setup", got.Status)
	}
	// Existing values are still preserved (not destructively cleared) / 已有值仍保留，不破坏性清空
	if got.AdminConfig["main"]["token"] != "secret" {
		t.Errorf("AdminConfig not preserved: %v", got.AdminConfig)
	}
}

func TestReinstallDevPlugin_RejectsNonDevPlugin(t *testing.T) {
	svc, st := newReinstallTestService(t)

	existing := &store.PluginInstance{
		ID:          "demo",
		PluginDir:   "/old/dir",
		Status:      store.StatusReady,
		IsDevPlugin: false, // installed via Web-UI / 经 Web-UI 安装
	}
	if err := st.SavePlugin(existing); err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}

	pkg := bootConfig()
	if _, err := svc.ReinstallDevPlugin(context.Background(), pkg, "/new/dir", existing); err == nil {
		t.Fatal("expected error for non-dev plugin, got nil")
	}

	// Store must be untouched / 不得改动 store
	saved, _ := st.GetPlugin("demo")
	if saved == nil || saved.PluginDir != "/old/dir" {
		t.Errorf("store should be unchanged: %+v", saved)
	}
}
