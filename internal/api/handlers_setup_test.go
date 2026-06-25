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

	"github.com/GriffinGuard/Griffino/internal/store"
)

func TestHandleSetupState(t *testing.T) {
	s := newTestServer(t, serverOpts{})

	// Fresh install: no users yet -> needsAdmin true, completed false.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/state", nil)
	rr := httptest.NewRecorder()
	s.handleSetupState(rr, req)
	assertStatus(t, rr, http.StatusOK)
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["needsAdmin"] != true || resp["completed"] != false {
		t.Fatalf("fresh state = %v, want needsAdmin=true completed=false", resp)
	}

	// After creating a user, needsAdmin becomes false.
	if _, err := s.st.CreateUser("admin", "password123", store.RoleAdmin, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	rr = httptest.NewRecorder()
	s.handleSetupState(rr, httptest.NewRequest(http.MethodGet, "/api/v1/setup/state", nil))
	assertStatus(t, rr, http.StatusOK)
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["needsAdmin"] != false {
		t.Fatalf("after user state = %v, want needsAdmin=false", resp)
	}
}

func TestHandleSetupStatus(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	rr := httptest.NewRecorder()
	s.handleSetupStatus(rr, httptest.NewRequest(http.MethodGet, "/api/v1/setup/status", nil))
	assertStatus(t, rr, http.StatusOK)
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["completed"] != false {
		t.Fatalf("completed = %v, want false", resp["completed"])
	}
}

func TestHandleSetupComplete(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	rr := httptest.NewRecorder()
	s.handleSetupComplete(rr, httptest.NewRequest(http.MethodPost, "/api/v1/setup/complete", nil))
	assertStatus(t, rr, http.StatusOK)
	completed, err := s.st.GetSetupCompleted()
	if err != nil || !completed {
		t.Fatalf("setup not marked completed: completed=%v err=%v", completed, err)
	}
}

func TestHandleSetupCreateAdmin(t *testing.T) {
	t.Run("success on fresh install", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin",
			strings.NewReader(`{"username":"root","password":"password123"}`))
		rr := httptest.NewRecorder()
		s.handleSetupCreateAdmin(rr, req)
		assertStatus(t, rr, http.StatusCreated)
		if u, err := s.st.GetUserByUsername("root"); err != nil || u == nil || u.Role != store.RoleAdmin {
			t.Fatalf("admin not created: %v %v", u, err)
		}
	})

	t.Run("conflict when a user exists", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		if _, err := s.st.CreateUser("admin", "password123", store.RoleAdmin, false); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin",
			strings.NewReader(`{"username":"root","password":"password123"}`))
		rr := httptest.NewRecorder()
		s.handleSetupCreateAdmin(rr, req)
		assertErrorCode(t, rr, http.StatusConflict, ErrAdminAlreadyExists)
	})

	t.Run("password too short", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin",
			strings.NewReader(`{"username":"root","password":"short"}`))
		rr := httptest.NewRecorder()
		s.handleSetupCreateAdmin(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrAuthPasswordTooShort)
	})

	t.Run("malformed body", func(t *testing.T) {
		s := newTestServer(t, serverOpts{})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/setup/admin", strings.NewReader(`{bad`))
		rr := httptest.NewRecorder()
		s.handleSetupCreateAdmin(rr, req)
		assertErrorCode(t, rr, http.StatusBadRequest, ErrAuthInvalidRequest)
	})
}
