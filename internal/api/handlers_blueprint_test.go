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

func bpSession(userID string) *auth.SessionData {
	return &auth.SessionData{UserID: userID, Username: userID, Role: "user"}
}

// createBlueprint drives handleCreateBlueprint and returns the created blueprint id.
func createBlueprint(t *testing.T, s *Server, userID, name string) string {
	t.Helper()
	body := `{"name":"` + name + `","trigger":{"eventType":"manual"},"nodes":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/blueprints", strings.NewReader(body))
	req = withSession(req, bpSession(userID))
	rr := httptest.NewRecorder()
	s.handleCreateBlueprint(rr, req)
	assertStatus(t, rr, http.StatusCreated)
	var bp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &bp); err != nil {
		t.Fatalf("decode created bp: %v", err)
	}
	id, _ := bp["id"].(string)
	if id == "" {
		t.Fatalf("created blueprint has no id: %v", bp)
	}
	return id
}

func TestHandleCreateBlueprint(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(t, serverOpts{withBP: true})
		createBlueprint(t, s, "u1", "My Flow")
	})

	t.Run("missing name", func(t *testing.T) {
		s := newTestServer(t, serverOpts{withBP: true})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/blueprints",
			strings.NewReader(`{"trigger":{"eventType":"manual"}}`))
		req = withSession(req, bpSession("u1"))
		rr := httptest.NewRecorder()
		s.handleCreateBlueprint(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrBlueprintInvalidRequest)
	})

	t.Run("missing trigger eventType", func(t *testing.T) {
		s := newTestServer(t, serverOpts{withBP: true})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/blueprints",
			strings.NewReader(`{"name":"x"}`))
		req = withSession(req, bpSession("u1"))
		rr := httptest.NewRecorder()
		s.handleCreateBlueprint(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrBlueprintInvalidRequest)
	})

	t.Run("malformed body", func(t *testing.T) {
		s := newTestServer(t, serverOpts{withBP: true})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/blueprints", strings.NewReader(`{bad`))
		req = withSession(req, bpSession("u1"))
		rr := httptest.NewRecorder()
		s.handleCreateBlueprint(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrBlueprintInvalidRequest)
	})
}

func TestHandleListBlueprints(t *testing.T) {
	s := newTestServer(t, serverOpts{withBP: true})
	createBlueprint(t, s, "u1", "a")
	createBlueprint(t, s, "u1", "b")
	createBlueprint(t, s, "u2", "c") // different user

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints", nil)
	req = withSession(req, bpSession("u1"))
	rr := httptest.NewRecorder()
	s.handleListBlueprints(rr, req)
	assertStatus(t, rr, http.StatusOK)
	var resp struct {
		Blueprints []map[string]any `json:"blueprints"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Blueprints) != 2 {
		t.Fatalf("expected 2 blueprints for u1, got %d", len(resp.Blueprints))
	}
}

func TestHandleGetBlueprint(t *testing.T) {
	s := newTestServer(t, serverOpts{withBP: true})
	id := createBlueprint(t, s, "u1", "a")

	t.Run("owner can get", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/"+id, nil)
		req.SetPathValue("id", id)
		req = withSession(req, bpSession("u1"))
		rr := httptest.NewRecorder()
		s.handleGetBlueprint(rr, req)
		assertStatus(t, rr, http.StatusOK)
	})

	t.Run("other user gets 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/"+id, nil)
		req.SetPathValue("id", id)
		req = withSession(req, bpSession("u2"))
		rr := httptest.NewRecorder()
		s.handleGetBlueprint(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrBlueprintNotFound)
	})

	t.Run("missing id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/ghost", nil)
		req.SetPathValue("id", "ghost")
		req = withSession(req, bpSession("u1"))
		rr := httptest.NewRecorder()
		s.handleGetBlueprint(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrBlueprintNotFound)
	})
}

func TestHandleUpdateBlueprint(t *testing.T) {
	s := newTestServer(t, serverOpts{withBP: true})
	id := createBlueprint(t, s, "u1", "old")

	t.Run("success", func(t *testing.T) {
		body := `{"name":"new","trigger":{"eventType":"manual"},"nodes":[]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/blueprints/"+id, strings.NewReader(body))
		req.SetPathValue("id", id)
		req = withSession(req, bpSession("u1"))
		rr := httptest.NewRecorder()
		s.handleUpdateBlueprint(rr, req)
		assertStatus(t, rr, http.StatusOK)
		bp, _ := s.bpStore.Get(id)
		if bp.Name != "new" {
			t.Fatalf("name not updated: %q", bp.Name)
		}
	})

	t.Run("other user 404", func(t *testing.T) {
		body := `{"name":"x","trigger":{"eventType":"manual"},"nodes":[]}`
		req := httptest.NewRequest(http.MethodPut, "/api/v1/blueprints/"+id, strings.NewReader(body))
		req.SetPathValue("id", id)
		req = withSession(req, bpSession("u2"))
		rr := httptest.NewRecorder()
		s.handleUpdateBlueprint(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrBlueprintNotFound)
	})
}

func TestHandleDeleteBlueprint(t *testing.T) {
	s := newTestServer(t, serverOpts{withBP: true})

	t.Run("success", func(t *testing.T) {
		id := createBlueprint(t, s, "u1", "a")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/blueprints/"+id, nil)
		req.SetPathValue("id", id)
		req = withSession(req, bpSession("u1"))
		rr := httptest.NewRecorder()
		s.handleDeleteBlueprint(rr, req)
		assertStatus(t, rr, http.StatusNoContent)
		if _, err := s.bpStore.Get(id); err == nil {
			t.Fatalf("blueprint still present after delete")
		}
	})

	t.Run("other user 404", func(t *testing.T) {
		id := createBlueprint(t, s, "u1", "b")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/blueprints/"+id, nil)
		req.SetPathValue("id", id)
		req = withSession(req, bpSession("u2"))
		rr := httptest.NewRecorder()
		s.handleDeleteBlueprint(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrBlueprintNotFound)
	})
}
