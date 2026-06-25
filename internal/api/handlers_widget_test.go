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
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/redis/go-redis/v9"
)

type componentDataCall struct {
	PluginID    string
	ComponentID string
	UserID      string
}

type fakeComponentDataRenderer struct {
	calls []componentDataCall
	fn    func(pluginID, componentID, userID string) (map[string]any, error)
}

func (f *fakeComponentDataRenderer) RenderComponentData(_ context.Context, pluginID, componentID, userID string) (map[string]any, error) {
	f.calls = append(f.calls, componentDataCall{PluginID: pluginID, ComponentID: componentID, UserID: userID})
	return f.fn(pluginID, componentID, userID)
}

func TestHandleGetWidgetDataComponentDataMerge(t *testing.T) {
	s, pluginDir, redisAddr := newWidgetTestServer(t)
	seedPluginDir(t, s, "p1", pluginDir, store.StatusRunning)

	s.componentData = &fakeComponentDataRenderer{fn: func(_, _, userID string) (map[string]any, error) {
		return map[string]any{
			"status":  "fresh",
			"options": []any{"m1", "m2"},
			"userId":  userID,
		}, nil
	}}

	rdb := newTestRedisClient(redisAddr)
	defer rdb.Close()
	if err := rdb.Set(context.Background(), "plugin:p1:state:u1:status", "stale", 0).Err(); err != nil {
		t.Fatalf("redis set: %v", err)
	}

	req := widgetDataRequest("p1", "w1", "u1")
	rr := httptest.NewRecorder()
	s.handleGetWidgetData(rr, req)
	assertStatus(t, rr, http.StatusOK)

	var resp struct {
		WidgetID string         `json:"widgetId"`
		Data     map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data["status"] != "fresh" {
		t.Fatalf("status = %v, want DataHandler override", resp.Data["status"])
	}
	if resp.Data["userId"] != "u1" {
		t.Fatalf("userId = %v, want u1", resp.Data["userId"])
	}
	if got := resp.Data["options"].([]any); len(got) != 2 || got[0] != "m1" {
		t.Fatalf("options = %#v", got)
	}
}

func TestHandleGetWidgetDataComponentDataFallbacks(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "not supported", err: errComponentDataNotSupported},
		{name: "rpc failure", err: errors.New("dial failed")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, pluginDir, redisAddr := newWidgetTestServer(t)
			seedPluginDir(t, s, "p1", pluginDir, store.StatusRunning)
			s.componentData = &fakeComponentDataRenderer{fn: func(_, _, _ string) (map[string]any, error) {
				return nil, tc.err
			}}

			rdb := newTestRedisClient(redisAddr)
			defer rdb.Close()
			if err := rdb.Set(context.Background(), "plugin:p1:state:u1:status", "redis-only", 0).Err(); err != nil {
				t.Fatalf("redis set: %v", err)
			}

			rr := httptest.NewRecorder()
			s.handleGetWidgetData(rr, widgetDataRequest("p1", "w1", "u1"))
			assertStatus(t, rr, http.StatusOK)
			var resp struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Data["status"] != "redis-only" {
				t.Fatalf("status = %v, want redis fallback", resp.Data["status"])
			}
		})
	}
}

func TestHandleGetWidgetDataComponentDataUserIsolation(t *testing.T) {
	s, pluginDir, _ := newWidgetTestServer(t)
	seedPluginDir(t, s, "p1", pluginDir, store.StatusRunning)
	fake := &fakeComponentDataRenderer{fn: func(_, _, userID string) (map[string]any, error) {
		return map[string]any{"userSpecific": userID}, nil
	}}
	s.componentData = fake

	for _, userID := range []string{"u1", "u2"} {
		rr := httptest.NewRecorder()
		s.handleGetWidgetData(rr, widgetDataRequest("p1", "w1", userID))
		assertStatus(t, rr, http.StatusOK)
		var resp struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Data["userSpecific"] != userID {
			t.Fatalf("userSpecific = %v, want %s", resp.Data["userSpecific"], userID)
		}
	}
	if fake.calls[0].UserID != "u1" || fake.calls[1].UserID != "u2" {
		t.Fatalf("calls = %+v", fake.calls)
	}
}

func newWidgetTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	s := newTestServer(t, serverOpts{})
	redisAddr := newTestRedis(t)
	s.statusViewReader = newStatusViewReader(redisAddr, "")
	return s, writePluginPackage(t, true), redisAddr
}

func widgetDataRequest(pluginID, widgetID, userID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/plugins/"+pluginID+"/widgets/"+widgetID+"/data", nil)
	req.SetPathValue("id", pluginID)
	req.SetPathValue("widgetId", widgetID)
	return withSession(req, &auth.SessionData{UserID: userID})
}

func seedPluginDir(t *testing.T, s *Server, id, pluginDir string, status store.PluginStatus) {
	t.Helper()
	if err := s.st.SavePlugin(&store.PluginInstance{
		ID:          id,
		PluginDir:   pluginDir,
		Status:      status,
		AdminConfig: map[string]map[string]string{},
	}); err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}
}

func writePluginPackage(t *testing.T, includeUserConfig bool) string {
	t.Helper()
	dir := t.TempDir()
	userConfigFile := ""
	if includeUserConfig {
		userConfigFile = `,"userConfig":"config.user.json"`
	}
	writeFile(t, filepath.Join(dir, "plugin.manifest.json"), `{
		"griffinoPluginManifestVersion":"1",
		"id":"p1",
		"pluginVersion":"1.0.0",
		"name":{"default":"Plugin"},
		"description":{"default":"Plugin"},
		"author":"test",
		"site":"",
		"capabilities":[],
		"configurationFiles":{"bootConfig":"config.boot.json","runtimeBoot":"plugin.boot.yml"`+userConfigFile+`},
		"components":[{"id":"w1","name":{"default":"Widget"},"refreshMs":1000,"root":{"type":"Text","bind":"status"}}]
	}`)
	writeFile(t, filepath.Join(dir, "config.boot.json"), `{
		"GriffinoPluginConfigVersion":"1",
		"pluginId":"p1",
		"pluginVersion":"1.0.0",
		"name":"Plugin",
		"site":"",
		"services":[{"id":"main","configs":[]}]
	}`)
	writeFile(t, filepath.Join(dir, "plugin.boot.yml"), `pluginBootSpecVersion: "1"
pluginId: p1
pluginVersion: 1.0.0
mainServiceId: main
services:
  main:
    image: test:latest
    environment: []
`)
	if includeUserConfig {
		writeFile(t, filepath.Join(dir, "config.user.json"), `{
			"GriffinoPluginConfigVersion":"1",
			"pluginId":"p1",
			"pluginVersion":"1.0.0",
			"name":"Plugin",
			"site":"",
			"configs":[{
				"key":"MODELS",
				"name":"Models",
				"description":"Model entries",
				"type":"group_array",
				"group":"Models",
				"optional":true,
				"minItems":1,
				"maxItems":2,
				"fields":[
					{"key":"name","name":"Name","description":"","type":"string","optional":true},
					{"key":"supportsVision","name":"Supports Vision","description":"","type":"boolean","optional":true}
				]
			},{"key":"TOKEN","name":"Token","description":"","type":"password","optional":true}]
		}`)
	}
	return dir
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newTestRedisClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}
