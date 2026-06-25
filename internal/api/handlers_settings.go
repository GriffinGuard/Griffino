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

	"github.com/GriffinGuard/Griffino/internal/store"
)

// handleGetSecuritySettings returns the current security policies.
//
//	@Summary	Get security policies
//	@Tags		Settings
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	store.SecurityPolicies
//	@Failure	500	{object}	api.AppError
//	@Router		/admin/settings/security [get]
func (s *Server) handleGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.GetSecurityPolicies()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSettingsFetchFailed, "Failed to read security settings")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handlePutSecuritySettings validates and persists new security policy values.
//
//	@Summary	Update security policies
//	@Tags		Settings
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		store.SecurityPolicies	true	"security policies"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	400		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/admin/settings/security [put]
func (s *Server) handlePutSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionTTLHours        *int `json:"sessionTtlHours"`
		MaxLoginAttempts       *int `json:"maxLoginAttempts"`
		LockoutDurationMinutes *int `json:"lockoutDurationMinutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrSettingsInvalidRequest, "Invalid request format")
		return
	}

	if req.SessionTTLHours == nil || req.MaxLoginAttempts == nil || req.LockoutDurationMinutes == nil {
		writeAppError(w, http.StatusBadRequest, ErrSettingsInvalidRequest, "All fields are required")
		return
	}
	if *req.SessionTTLHours < 1 || *req.SessionTTLHours > 8760 {
		writeAppError(w, http.StatusBadRequest, ErrSettingsInvalidRequest, "sessionTtlHours must be between 1 and 8760")
		return
	}
	if *req.MaxLoginAttempts < 1 || *req.MaxLoginAttempts > 100 {
		writeAppError(w, http.StatusBadRequest, ErrSettingsInvalidRequest, "maxLoginAttempts must be between 1 and 100")
		return
	}
	if *req.LockoutDurationMinutes < 1 || *req.LockoutDurationMinutes > 1440 {
		writeAppError(w, http.StatusBadRequest, ErrSettingsInvalidRequest, "lockoutDurationMinutes must be between 1 and 1440")
		return
	}

	if err := s.st.SetSecurityPolicies(store.SecurityPolicies{
		SessionTTLHours:        *req.SessionTTLHours,
		MaxLoginAttempts:       *req.MaxLoginAttempts,
		LockoutDurationMinutes: *req.LockoutDurationMinutes,
	}); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSettingsSaveFailed, "Failed to save security settings")
		return
	}
	s.writeAuditLog(r, "UPDATE_SECURITY_SETTINGS", "settings/security", "", "info")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
