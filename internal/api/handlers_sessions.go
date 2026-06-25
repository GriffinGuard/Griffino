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
	"net/http"
	"strings"

	"github.com/GriffinGuard/Griffino/internal/auth"
)

// handleListMySessions returns all active sessions for the current user.
//
//	@Summary	List active sessions
//	@Tags		Sessions
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/me/sessions [get]
func (s *Server) handleListMySessions(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	currentToken := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

	summaries, err := s.sessionMgr.ListByUser(r.Context(), session.UserID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSessionListFailed, "Failed to list sessions")
		return
	}

	for i := range summaries {
		if summaries[i].ID == auth.SessionID(currentToken) {
			summaries[i].IsCurrent = true
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"sessions": summaries})
}

// handleRevokeMySession revokes a specific session belonging to the current user.
//
//	@Summary	Revoke a session
//	@Tags		Sessions
//	@Produce	json
//	@Security	BearerAuth
//	@Param		sessionID	path		string	true	"Session ID to revoke"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	404		{object}	api.AppError
//	@Router		/me/sessions/{sessionID} [delete]
func (s *Server) handleRevokeMySession(w http.ResponseWriter, r *http.Request) {
	caller := r.Context().Value(sessionKey).(*auth.SessionData)
	sessionID := r.PathValue("sessionID")

	deleted, err := s.sessionMgr.DeleteByUserSessionID(r.Context(), caller.UserID, sessionID)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSessionListFailed, "Failed to revoke session")
		return
	}
	if !deleted {
		writeAppError(w, http.StatusNotFound, ErrSessionNotFound, "Session not found or already expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
