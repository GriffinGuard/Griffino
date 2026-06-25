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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GriffinGuard/Griffino/internal/store"
)

func seedPlugin(t *testing.T, s *Server, id string, status store.PluginStatus) {
	t.Helper()
	if err := s.st.SavePlugin(&store.PluginInstance{
		ID:          id,
		PluginDir:   "/nonexistent/" + id,
		Status:      status,
		AdminConfig: map[string]map[string]string{},
	}); err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}
}

func TestHandleListPlugins(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	seedPlugin(t, s, "p1", store.StatusReady)
	seedPlugin(t, s, "p2", store.StatusRunning)

	rr := httptest.NewRecorder()
	s.handleListPlugins(rr, httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil))
	assertStatus(t, rr, http.StatusOK)
	var resp struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(resp.Plugins))
	}
}

func TestHandleGetPlugin(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	seedPlugin(t, s, "p1", store.StatusReady)

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/p1", nil)
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleGetPlugin(rr, req)
		assertStatus(t, rr, http.StatusOK)
		var dto map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &dto)
		if dto["id"] != "p1" {
			t.Fatalf("unexpected dto: %v", dto)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/ghost", nil)
		req.SetPathValue("id", "ghost")
		rr := httptest.NewRecorder()
		s.handleGetPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrPluginNotFound)
	})
}

// TestHandleConfigPlugin covers the "save" action and validation branches. The
// save_and_start / save_and_restart branches load the on-disk manifest and are
// covered indirectly; here we exercise the store-only paths.
func TestHandleConfigPlugin(t *testing.T) {
	t.Run("save success", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		seedPlugin(t, s, "p1", store.StatusReady)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/config",
			strings.NewReader(`{"action":"save","config":{"svc":{"key":"val"}}}`))
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleConfigPlugin(rr, req)
		assertStatus(t, rr, http.StatusOK)
		p, _ := s.st.GetPlugin("p1")
		if p.AdminConfig["svc"]["key"] != "val" {
			t.Fatalf("config not persisted: %+v", p.AdminConfig)
		}
	})

	t.Run("plugin not found", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/ghost/config",
			strings.NewReader(`{"action":"save"}`))
		req.SetPathValue("id", "ghost")
		rr := httptest.NewRecorder()
		s.handleConfigPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrPluginNotFound)
	})

	t.Run("malformed body", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		seedPlugin(t, s, "p1", store.StatusReady)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/config", strings.NewReader(`{bad`))
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleConfigPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrAuthInvalidRequest)
	})

	t.Run("save_and_restart on non-running -> conflict", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		seedPlugin(t, s, "p1", store.StatusReady)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/config",
			strings.NewReader(`{"action":"save_and_restart"}`))
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleConfigPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusConflict, ErrPluginNotRunning)
	})

	t.Run("save_and_restart success via fake", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		seedPlugin(t, s, "p1", store.StatusRunning)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/config",
			strings.NewReader(`{"action":"save_and_restart"}`))
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleConfigPlugin(rr, req)
		assertStatus(t, rr, http.StatusAccepted)
	})

	t.Run("save_and_restart fake error", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{stopThenStart: errors.New("boom")}})
		seedPlugin(t, s, "p1", store.StatusRunning)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/config",
			strings.NewReader(`{"action":"save_and_restart"}`))
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleConfigPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrPluginStartFailed)
	})
}

func TestHandleStartPlugin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/start", nil)
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleStartPlugin(rr, req)
		assertStatus(t, rr, http.StatusAccepted)
	})

	t.Run("service error", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{startAsyncErr: errors.New("nope")}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/start", nil)
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleStartPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrPluginStartFailed)
	})
}

func TestHandleStopPlugin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/stop", nil)
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleStopPlugin(rr, req)
		assertStatus(t, rr, http.StatusOK)
	})

	t.Run("service error", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{stopErr: errors.New("nope")}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/p1/stop", nil)
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleStopPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusInternalServerError, ErrPluginStopFailed)
	})
}

func TestHandleUninstallPlugin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		seedPlugin(t, s, "p1", store.StatusStopped)
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/p1", nil)
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleUninstallPlugin(rr, req)
		assertStatus(t, rr, http.StatusNoContent)
	})

	t.Run("not found", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{}})
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/ghost", nil)
		req.SetPathValue("id", "ghost")
		rr := httptest.NewRecorder()
		s.handleUninstallPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrPluginNotFound)
	})

	t.Run("service error", func(t *testing.T) {
		s := newTestServer(t, serverOpts{pluginSvc: &fakePluginService{uninstallErr: errors.New("nope")}})
		seedPlugin(t, s, "p1", store.StatusStopped)
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/plugins/p1", nil)
		req.SetPathValue("id", "p1")
		rr := httptest.NewRecorder()
		s.handleUninstallPlugin(rr, req)
		assertErrorCode(t, rr, http.StatusInternalServerError, ErrPluginUninstallFailed)
	})
}
