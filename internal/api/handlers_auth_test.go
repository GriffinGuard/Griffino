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

func TestHandleLogin(t *testing.T) {
	s := newTestServer(t, serverOpts{withSession: true})
	if _, err := s.st.CreateUser("alice", "password123", store.RoleAdmin, false); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   ErrorCode // empty means success
	}{
		{"success", `{"username":"alice","password":"password123"}`, http.StatusOK, ""},
		{"wrong password", `{"username":"alice","password":"nope"}`, http.StatusUnauthorized, ErrAuthInvalidCredentials},
		{"unknown user", `{"username":"ghost","password":"whatever"}`, http.StatusUnauthorized, ErrAuthInvalidCredentials},
		{"malformed json", `{not json`, http.StatusBadRequest, ErrAuthInvalidRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(tc.body))
			rr := httptest.NewRecorder()
			s.handleLogin(rr, req)
			if tc.wantCode == "" {
				assertStatus(t, rr, tc.wantStatus)
				var resp map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if resp["token"] == "" || resp["username"] != "alice" {
					t.Fatalf("unexpected login payload: %v", resp)
				}
			} else {
				assertErrorCode(t, rr, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

func TestHandleLogout(t *testing.T) {
	s := newTestServer(t, serverOpts{withSession: true})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer sometoken")
	rr := httptest.NewRecorder()
	s.handleLogout(rr, req)
	assertStatus(t, rr, http.StatusOK)
}

func TestHandleMe(t *testing.T) {
	s := newTestServer(t, serverOpts{})
	u, err := s.st.CreateUser("bob", "password123", store.RoleUser, false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req = withSession(req, &auth.SessionData{UserID: u.ID, Username: "bob", Role: string(store.RoleUser)})
	rr := httptest.NewRecorder()
	s.handleMe(rr, req)
	assertStatus(t, rr, http.StatusOK)

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["username"] != "bob" || resp["role"] != string(store.RoleUser) {
		t.Fatalf("unexpected me payload: %v", resp)
	}
}

func TestHandleChangePassword(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   ErrorCode
	}{
		{"success", `{"oldPassword":"password123","newPassword":"newpassword456"}`, http.StatusOK, ""},
		{"too short", `{"oldPassword":"password123","newPassword":"short"}`, http.StatusBadRequest, ErrAuthPasswordTooShort},
		{"wrong old", `{"oldPassword":"wrong","newPassword":"newpassword456"}`, http.StatusForbidden, ErrAuthWrongPassword},
		{"malformed", `{nope`, http.StatusBadRequest, ErrAuthInvalidRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, serverOpts{})
			if _, err := s.st.CreateUser("carol", "password123", store.RoleUser, true); err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(tc.body))
			req = withSession(req, &auth.SessionData{Username: "carol", Role: string(store.RoleUser)})
			rr := httptest.NewRecorder()
			s.handleChangePassword(rr, req)
			if tc.wantCode == "" {
				assertStatus(t, rr, tc.wantStatus)
				// Verify the new password now authenticates.
				if _, err := s.st.VerifyPassword("carol", "newpassword456"); err != nil {
					t.Fatalf("new password not accepted: %v", err)
				}
			} else {
				assertErrorCode(t, rr, tc.wantStatus, tc.wantCode)
			}
		})
	}
}
