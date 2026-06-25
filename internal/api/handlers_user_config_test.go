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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

func TestHandleGetUserConfigSchemaGroupArray(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	pluginDir := writePluginPackage(t, true)
	seedPluginDir(t, s, "p1", pluginDir, store.StatusReady)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/p1/user-config", nil)
	req.SetPathValue("id", "p1")
	rr := httptest.NewRecorder()
	s.handleGetUserConfigSchema(rr, req)
	assertStatus(t, rr, http.StatusOK)

	var resp struct {
		Configs []map[string]any `json:"configs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Configs) == 0 || resp.Configs[0]["type"] != "group_array" {
		t.Fatalf("configs = %#v", resp.Configs)
	}
	if got := resp.Configs[0]["fields"].([]any); len(got) != 2 {
		t.Fatalf("fields = %#v", got)
	}
	if resp.Configs[0]["minItems"].(float64) != 1 || resp.Configs[0]["maxItems"].(float64) != 2 {
		t.Fatalf("min/max not preserved: %#v", resp.Configs[0])
	}
}

func TestHandleSetUserConfigValuesGroupArrayRoundTrip(t *testing.T) {
	s, pluginDir := newUserConfigTestServer(t)
	seedPluginDir(t, s, "p1", pluginDir, store.StatusReady)

	body := `{
		"MODELS":[
			{"name":"vision","supportsVision":true,"ignored":"drop-me"},
			{"name":"text","supportsVision":false}
		],
		"TOKEN":"secret",
		"UNKNOWN":"drop-me"
	}`
	req := userConfigSetRequest("p1", "u1", body)
	rr := httptest.NewRecorder()
	s.handleSetUserConfigValues(rr, req)
	assertStatus(t, rr, http.StatusOK)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/p1/user-config/values", nil)
	getReq.SetPathValue("id", "p1")
	getReq = withSession(getReq, &auth.SessionData{UserID: "u1"})
	getRR := httptest.NewRecorder()
	s.handleGetUserConfigValues(getRR, getReq)
	assertStatus(t, getRR, http.StatusOK)

	var resp struct {
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	models := resp.Values["MODELS"].([]any)
	first := models[0].(map[string]any)
	if first["name"] != "vision" || first["supportsVision"] != true {
		t.Fatalf("first model = %#v", first)
	}
	if _, ok := first["ignored"]; ok {
		t.Fatalf("unexpected nested unknown field: %#v", first)
	}
	if _, ok := resp.Values["UNKNOWN"]; ok {
		t.Fatalf("unexpected unknown top-level field: %#v", resp.Values)
	}
	if resp.Values["TOKEN"] != passwordPlaceholder {
		t.Fatalf("TOKEN = %v, want placeholder", resp.Values["TOKEN"])
	}
}

func TestHandleSetUserConfigValuesGroupArrayMinMax(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "too few", body: `{"MODELS":[]}`},
		{name: "too many", body: `{"MODELS":[{"name":"a"},{"name":"b"},{"name":"c"}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, pluginDir := newUserConfigTestServer(t)
			seedPluginDir(t, s, "p1", pluginDir, store.StatusReady)

			rr := httptest.NewRecorder()
			s.handleSetUserConfigValues(rr, userConfigSetRequest("p1", "u1", tc.body))
			assertErrorCode(t, rr, http.StatusBadRequest, ErrUserConfigInvalidRequest)
		})
	}
}

func TestNormalizeUserConfigValueGroupArrayFieldTypes(t *testing.T) {
	boolField := manifest.ConfigParam{Key: "flag", Type: manifest.ConfigTypeBoolean, Optional: true}
	intField := manifest.ConfigParam{Key: "count", Type: manifest.ConfigTypeInt, Optional: true}
	strField := manifest.ConfigParam{Key: "label", Type: manifest.ConfigTypeString, Optional: true}
	param := manifest.ConfigParam{
		Key:    "ITEMS",
		Type:   manifest.ConfigTypeGroupArray,
		Fields: []manifest.ConfigParam{boolField, intField, strField},
	}

	tests := []struct {
		name    string
		item    map[string]any
		wantErr bool
	}{
		{"valid types", map[string]any{"flag": true, "count": float64(3), "label": "x"}, false},
		{"bool field wrong type", map[string]any{"flag": "yes"}, true},
		{"int field non-integer float", map[string]any{"count": float64(1.5)}, true},
		{"string field wrong type", map[string]any{"label": 42.0}, true},
		{"unknown key dropped silently", map[string]any{"unknown": "val"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeUserConfigValue(param, []any{tc.item})
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tc.wantErr, err)
			}
		})
	}
}

func TestNormalizeUserConfigValueGroupArrayRequiredField(t *testing.T) {
	param := manifest.ConfigParam{
		Key:  "ITEMS",
		Type: manifest.ConfigTypeGroupArray,
		Fields: []manifest.ConfigParam{
			{Key: "name", Type: manifest.ConfigTypeString, Optional: false},
			{Key: "note", Type: manifest.ConfigTypeString, Optional: true},
		},
	}

	t.Run("required field present", func(t *testing.T) {
		_, err := normalizeUserConfigValue(param, []any{map[string]any{"name": "x"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("required field missing", func(t *testing.T) {
		_, err := normalizeUserConfigValue(param, []any{map[string]any{"note": "only optional"}})
		if err == nil {
			t.Fatal("expected error for missing required field")
		}
	})
}

func TestHandleGetUserConfigValuesPasswordMasking(t *testing.T) {
	s, pluginDir := newUserConfigTestServer(t)
	seedPluginDir(t, s, "p1", pluginDir, store.StatusReady)

	// Store values including a plaintext password and a group_array (no password sub-field in fixture, but TOKEN covers top-level)
	setReq := userConfigSetRequest("p1", "u1", `{"TOKEN":"mysecret","MODELS":[{"name":"m1","supportsVision":false}]}`)
	rr := httptest.NewRecorder()
	s.handleSetUserConfigValues(rr, setReq)
	assertStatus(t, rr, http.StatusOK)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/p1/user-config/values", nil)
	getReq.SetPathValue("id", "p1")
	getReq = withSession(getReq, &auth.SessionData{UserID: "u1"})
	getRR := httptest.NewRecorder()
	s.handleGetUserConfigValues(getRR, getReq)
	assertStatus(t, getRR, http.StatusOK)

	var resp struct {
		Values map[string]any `json:"values"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Values["TOKEN"] != passwordPlaceholder {
		t.Fatalf("TOKEN = %v, want placeholder", resp.Values["TOKEN"])
	}
	// MODELS is group_array; no password sub-field in fixture, so name should still be readable
	models := resp.Values["MODELS"].([]any)
	if models[0].(map[string]any)["name"] != "m1" {
		t.Fatalf("MODELS[0].name = %v", models[0].(map[string]any)["name"])
	}
}

func TestHandleSetUserConfigValuesPasswordPlaceholderPreserved(t *testing.T) {
	s, pluginDir := newUserConfigTestServer(t)
	seedPluginDir(t, s, "p1", pluginDir, store.StatusReady)

	// Store a real password
	rr := httptest.NewRecorder()
	s.handleSetUserConfigValues(rr, userConfigSetRequest("p1", "u1", `{"TOKEN":"real-secret"}`))
	assertStatus(t, rr, http.StatusOK)

	// Submit placeholder (simulating a UI round-trip after GET /values returned the masked value)
	rr2 := httptest.NewRecorder()
	s.handleSetUserConfigValues(rr2, userConfigSetRequest("p1", "u1", `{"TOKEN":"`+passwordPlaceholder+`"}`))
	assertStatus(t, rr2, http.StatusOK)

	// Read raw value from store; it must still be the original secret, not the placeholder
	stored, err := s.userConfigStore.Get(context.Background(), "u1", "p1")
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if stored["TOKEN"] != "real-secret" {
		t.Fatalf("stored TOKEN = %v, want real-secret (placeholder must not overwrite secret)", stored["TOKEN"])
	}
}

func newUserConfigTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s := newTestServer(t, serverOpts{})
	redisAddr := newTestRedis(t)
	s.userConfigStore = store.NewUserConfigStore(redisAddr, "")
	return s, writePluginPackage(t, true)
}

func userConfigSetRequest(pluginID, userID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plugins/"+pluginID+"/user-config/values", strings.NewReader(body))
	req.SetPathValue("id", pluginID)
	return withSession(req, &auth.SessionData{UserID: userID})
}
