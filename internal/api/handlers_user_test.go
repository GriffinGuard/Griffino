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
	"github.com/GriffinGuard/Griffino/internal/store"
)

func TestHandleListUsers(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	if _, err := s.st.CreateUser("admin", "password123", store.RoleAdmin, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.st.CreateUser("u1", "password123", store.RoleUser, true); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	rr := httptest.NewRecorder()
	s.handleListUsers(rr, httptest.NewRequest(http.MethodGet, "/api/v1/users", nil))
	assertStatus(t, rr, http.StatusOK)
	var resp struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(resp.Users))
	}
	// Password hash must never be returned.
	for _, u := range resp.Users {
		if _, ok := u["passwordHash"]; ok {
			t.Fatalf("passwordHash leaked in list users")
		}
	}
}

func TestHandleCreateUser(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"username":"newbie"}`))
		rr := httptest.NewRecorder()
		s.handleCreateUser(rr, req)
		assertStatus(t, rr, http.StatusOK)
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["username"] != "newbie" || resp["tempPassword"] == "" {
			t.Fatalf("unexpected create payload: %v", resp)
		}
	})

	t.Run("missing username", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{}`))
		rr := httptest.NewRecorder()
		s.handleCreateUser(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrUserInvalidRequest)
	})

	t.Run("conflict", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		if _, err := s.st.CreateUser("dup", "password123", store.RoleUser, false); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(`{"username":"dup"}`))
		rr := httptest.NewRecorder()
		s.handleCreateUser(rr, req)
		assertErrorCode(t, rr, http.StatusConflict, ErrUsernameTaken)
	})
}

func TestHandleUpdateUser(t *testing.T) {
	t.Run("disable other user", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		if _, err := s.st.CreateUser("target", "password123", store.RoleUser, false); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/target", strings.NewReader(`{"disabled":true}`))
		req.SetPathValue("username", "target")
		req = withSession(req, &auth.SessionData{Username: "admin", Role: string(store.RoleAdmin)})
		rr := httptest.NewRecorder()
		s.handleUpdateUser(rr, req)
		assertStatus(t, rr, http.StatusOK)
		u, _ := s.st.GetUserByUsername("target")
		if u == nil || !u.Disabled {
			t.Fatalf("target not disabled: %v", u)
		}
	})

	t.Run("cannot self disable", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		if _, err := s.st.CreateUser("admin", "password123", store.RoleAdmin, false); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/admin", strings.NewReader(`{"disabled":true}`))
		req.SetPathValue("username", "admin")
		req = withSession(req, &auth.SessionData{Username: "admin", Role: string(store.RoleAdmin)})
		rr := httptest.NewRecorder()
		s.handleUpdateUser(rr, req)
		assertErrorCode(t, rr, http.StatusForbidden, ErrCannotDisableSelf)
	})

	t.Run("not found", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/ghost", strings.NewReader(`{"disabled":true}`))
		req.SetPathValue("username", "ghost")
		req = withSession(req, &auth.SessionData{Username: "admin", Role: string(store.RoleAdmin)})
		rr := httptest.NewRecorder()
		s.handleUpdateUser(rr, req)
		assertErrorCode(t, rr, http.StatusNotFound, ErrUserNotFound)
	})

	t.Run("reset password returns temp", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		if _, err := s.st.CreateUser("target", "password123", store.RoleUser, false); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/target", strings.NewReader(`{"resetPassword":true}`))
		req.SetPathValue("username", "target")
		req = withSession(req, &auth.SessionData{Username: "admin", Role: string(store.RoleAdmin)})
		rr := httptest.NewRecorder()
		s.handleUpdateUser(rr, req)
		assertStatus(t, rr, http.StatusOK)
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["tempPassword"] == "" || resp["tempPassword"] == nil {
			t.Fatalf("expected tempPassword in reset response: %v", resp)
		}
	})
}

func TestHandleUpdateUserProfile(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	if _, err := s.st.CreateUser("target", "password123", store.RoleUser, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/target/profile",
		strings.NewReader(`{"email":"t@example.com","displayName":"Target"}`))
	req.SetPathValue("username", "target")
	rr := httptest.NewRecorder()
	s.handleUpdateUserProfile(rr, req)
	assertStatus(t, rr, http.StatusOK)
	u, _ := s.st.GetUserByUsername("target")
	if u.Email != "t@example.com" || u.DisplayName != "Target" {
		t.Fatalf("profile not updated: %+v", u)
	}
}

func TestHandleDeleteUser(t *testing.T) {
	t.Run("cannot self delete", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/admin", nil)
		req.SetPathValue("username", "admin")
		req = withSession(req, &auth.SessionData{Username: "admin", Role: string(store.RoleAdmin)})
		rr := httptest.NewRecorder()
		s.handleDeleteUser(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrUserCannotSelfDelete)
	})

	t.Run("success", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		if _, err := s.st.CreateUser("target", "password123", store.RoleUser, false); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/target", nil)
		req.SetPathValue("username", "target")
		req = withSession(req, &auth.SessionData{Username: "admin", Role: string(store.RoleAdmin)})
		rr := httptest.NewRecorder()
		s.handleDeleteUser(rr, req)
		assertStatus(t, rr, http.StatusOK)
		if u, _ := s.st.GetUserByUsername("target"); u != nil {
			t.Fatalf("user not deleted")
		}
	})
}
