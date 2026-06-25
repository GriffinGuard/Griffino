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
	"strings"

	"github.com/GriffinGuard/Griffino/internal/store"
)

// handleSetupStatus reports whether the first-run setup wizard has been
// completed. The web console uses this (instead of browser localStorage) to
// decide whether to show the onboarding wizard.
//
//	@Summary	Get setup status
//	@Tags		Setup
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/setup/status [get]
func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	completed, err := s.st.GetSetupCompleted()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to read setup status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": completed})
}

// handleSetupState is the UNAUTHENTICATED pre-login probe the web console calls
// on load. needsAdmin is true on a fresh install (no users yet, e.g. a GUI
// installer started the daemon with --admin-init=web), telling the wizard to
// show a create-admin step before any login.
//
//	@Summary	Get pre-login setup state
//	@Tags		Setup
//	@Produce	json
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/setup/state [get]
func (s *Server) handleSetupState(w http.ResponseWriter, r *http.Request) {
	hasUser, err := s.st.HasAnyUser()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to read setup state")
		return
	}
	completed, err := s.st.GetSetupCompleted()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to read setup state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"needsAdmin": !hasUser,
		"completed":  completed,
	})
}

// handleSetupCreateAdmin creates the very first admin account. It is
// UNAUTHENTICATED but strictly gated to the no-users state, so it can never be
// used to escalate once an admin exists.
//
//	@Summary	Create first admin
//	@Tags		Setup
//	@Accept		json
//	@Produce	json
//	@Param		body	body		object	true	"username (optional, defaults to admin) and password"
//	@Success	201		{object}	map[string]interface{}
//	@Failure	400		{object}	api.AppError
//	@Failure	409		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/setup/admin [post]
func (s *Server) handleSetupCreateAdmin(w http.ResponseWriter, r *http.Request) {
	hasUser, err := s.st.HasAnyUser()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to read setup state")
		return
	}
	if hasUser {
		writeAppError(w, http.StatusConflict, ErrAdminAlreadyExists, "An admin account already exists")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrAuthInvalidRequest, "Invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		req.Username = "admin"
	}
	if len(req.Password) < 8 {
		writeAppError(w, http.StatusBadRequest, ErrAuthPasswordTooShort, "Password must be at least 8 characters")
		return
	}

	if _, err := s.st.CreateUser(req.Username, req.Password, store.RoleAdmin, false); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrAuthPasswordSaveFailed, "Failed to create admin account")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"username": req.Username})
}

// handleSetupReset resets the first-run setup wizard so it runs again.
//
//	@Summary	Reset setup wizard
//	@Tags		Setup
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/setup/reset [post]
func (s *Server) handleSetupReset(w http.ResponseWriter, r *http.Request) {
	if err := s.st.SetSetupCompleted(false); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to reset setup status")
		return
	}
	s.writeAuditLog(r, "SETUP_RESET", "setup", "", "warning")
	writeJSON(w, http.StatusOK, map[string]any{"completed": false})
}

// handleSetupComplete marks the first-run setup wizard as completed.
//
//	@Summary	Complete setup
//	@Tags		Setup
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/setup/complete [post]
func (s *Server) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if err := s.st.SetSetupCompleted(true); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to persist setup status")
		return
	}
	s.writeAuditLog(r, "SETUP_COMPLETE", "setup", "", "info")
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}
