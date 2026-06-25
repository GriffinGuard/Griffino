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
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
)

// writeAuditLog is a fire-and-forget helper that records an audit event.
// Errors are logged but never returned so they never block a business response / 记录审计事件，错误仅日志不阻断业务
func (s *Server) writeAuditLog(r *http.Request, action, resource, detail, level string) {
	actor := "unknown"
	if sess, ok := r.Context().Value(sessionKey).(*auth.SessionData); ok && sess != nil {
		actor = sess.Username
	}
	entry := &store.AuditLog{
		Actor:    actor,
		Action:   action,
		Resource: resource,
		Detail:   detail,
		Level:    level,
	}
	if err := s.st.CreateAuditLog(entry); err != nil {
		slog.Warn("audit log write failed", "action", action, "err", err)
	}
}

// handleListAuditLogs returns a paginated list of audit log entries with optional filtering.
//
//	@Summary	List audit logs
//	@Tags		Admin
//	@Produce	json
//	@Security	BearerAuth
//	@Param		page		query	int		false	"Page number (1-based)"
//	@Param		pageSize	query	int		false	"Items per page (default 20, max 100)"
//	@Param		actor		query	string	false	"Filter by actor username"
//	@Param		action		query	string	false	"Filter by action type"
//	@Param		resource	query	string	false	"Filter by resource"
//	@Param		from		query	string	false	"Start time (ISO 8601)"
//	@Param		to			query	string	false	"End time (ISO 8601)"
//	@Success	200			{object}	map[string]interface{}
//	@Failure	500			{object}	api.AppError
//	@Router		/audit-logs [get]
func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("pageSize"))

	f := store.AuditFilter{
		Actor:    q.Get("actor"),
		Action:   q.Get("action"),
		Resource: q.Get("resource"),
	}
	if s := q.Get("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.From = t
		}
	}
	if s := q.Get("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.To = t
		}
	}

	items, total, err := s.st.ListAuditLogs(f, page, pageSize)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to list audit logs")
		return
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}
