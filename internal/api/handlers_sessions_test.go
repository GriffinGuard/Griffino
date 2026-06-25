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
	"testing"
	"time"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
)

func TestHandleListMySessionsDoesNotExposeBearerToken(t *testing.T) {
	s := newTestServer(t, serverOpts{withSession: true})
	u, err := s.st.CreateUser("alice", "password123", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := s.sessionMgr.Create(t.Context(), auth.SessionData{
		UserID:   u.ID,
		Username: u.Username,
		Role:     string(u.Role),
	}, time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req = withSession(req, &auth.SessionData{UserID: u.ID, Username: u.Username, Role: string(u.Role)})
	rr := httptest.NewRecorder()
	s.handleListMySessions(rr, req)
	assertStatus(t, rr, http.StatusOK)

	var resp struct {
		Sessions []struct {
			ID        string `json:"id"`
			Token     string `json:"token"`
			IsCurrent bool   `json:"isCurrent"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].Token != "" {
		t.Fatalf("session response leaked bearer token")
	}
	if resp.Sessions[0].ID != auth.SessionID(token) {
		t.Fatalf("session ID = %q, want %q", resp.Sessions[0].ID, auth.SessionID(token))
	}
	if !resp.Sessions[0].IsCurrent {
		t.Fatalf("current session was not marked")
	}
}

func TestHandleRevokeMySessionByPublicID(t *testing.T) {
	s := newTestServer(t, serverOpts{withSession: true})
	u, err := s.st.CreateUser("alice", "password123", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, err := s.sessionMgr.Create(t.Context(), auth.SessionData{
		UserID:   u.ID,
		Username: u.Username,
		Role:     string(u.Role),
	}, time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/me/sessions/"+auth.SessionID(token), nil)
	req.SetPathValue("sessionID", auth.SessionID(token))
	req = withSession(req, &auth.SessionData{UserID: u.ID, Username: u.Username, Role: string(u.Role)})
	rr := httptest.NewRecorder()
	s.handleRevokeMySession(rr, req)
	assertStatus(t, rr, http.StatusOK)

	got, err := s.sessionMgr.Get(t.Context(), token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatalf("session still exists after revoke")
	}
}
