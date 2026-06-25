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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GriffinGuard/Griffino/internal/auth"
)

func TestHandleGetRoutesEmpty(t *testing.T) {
	s := newTestServer(t, serverOpts{withRoutes: true})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/routes", nil)
	req = withSession(req, &auth.SessionData{UserID: "u1", Username: "u1"})
	rr := httptest.NewRecorder()
	s.handleGetRoutes(rr, req)
	assertStatus(t, rr, http.StatusOK)
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["routes"]; !ok {
		t.Fatalf("expected routes key, got %v", resp)
	}
}

func TestHandleSetRoutes(t *testing.T) {
	t.Run("success roundtrip", func(t *testing.T) {
		s := newTestServer(t, serverOpts{withRoutes: true})
		body := `{"routes":[{"pluginId":"a","capabilityType":"chat","strategy":"fallback","providers":[]}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/routes", strings.NewReader(body))
		req = withSession(req, &auth.SessionData{UserID: "u1", Username: "u1"})
		rr := httptest.NewRecorder()
		s.handleSetRoutes(rr, req)
		assertStatus(t, rr, http.StatusOK)

		// Read them back through the handler.
		gr := httptest.NewRequest(http.MethodGet, "/api/v1/users/routes", nil)
		gr = withSession(gr, &auth.SessionData{UserID: "u1", Username: "u1"})
		grr := httptest.NewRecorder()
		s.handleGetRoutes(grr, gr)
		assertStatus(t, grr, http.StatusOK)
		var resp struct {
			Routes []map[string]any `json:"routes"`
		}
		_ = json.Unmarshal(grr.Body.Bytes(), &resp)
		if len(resp.Routes) != 1 {
			t.Fatalf("expected 1 route after set, got %d", len(resp.Routes))
		}
	})

	t.Run("malformed body", func(t *testing.T) {
		s := newTestServer(t, serverOpts{withRoutes: true})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/routes", strings.NewReader(`{bad`))
		req = withSession(req, &auth.SessionData{UserID: "u1", Username: "u1"})
		rr := httptest.NewRecorder()
		s.handleSetRoutes(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrRouteInvalidRequest)
	})
}
