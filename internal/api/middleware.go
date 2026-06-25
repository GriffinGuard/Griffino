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
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/GriffinGuard/Griffino/internal/auth"
)

type contextKey string

const sessionKey contextKey = "session"

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract token / 提取 token
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeAppError(w, http.StatusUnauthorized, ErrAuthNotLoggedIn, "Not logged in")
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")

		// Verify session / 验证 session
		session, err := s.sessionMgr.Get(r.Context(), token)
		if err != nil || session == nil {
			writeAppError(w, http.StatusUnauthorized, ErrAuthTokenInvalid, "Token is invalid or expired")
			return
		}

		// Renew session with the currently configured TTL / 按当前配置 TTL 续期
		_ = s.sessionMgr.Refresh(r.Context(), token, s.securityTTL())

		// Inject into context / 注入到 context
		ctx := context.WithValue(r.Context(), sessionKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := r.Context().Value(sessionKey).(*auth.SessionData)
		if !ok || session == nil {
			writeAppError(w, http.StatusUnauthorized, ErrAuthNotLoggedIn, "Not logged in")
			return
		}
		if session.Role != "admin" {
			writeAppError(w, http.StatusForbidden, ErrAuthPermissionDenied, "Permission denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 本机定位：只放行来自 localhost / 127.0.0.1 / [::1] 的跨源请求（任意端口），
		// not wildcard "*". Same-origin requests (embedded Web-UI) have no Origin header and don't need CORS / 而非通配 "*"，同源请求（内嵌 Web-UI）不带 Origin，无需 CORS 头.
		origin := r.Header.Get("Origin")
		if origin != "" && isLocalOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		// Return immediately for preflight requests / 预检请求直接返回
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isLocalOrigin reports whether the Origin points to a local loopback address (any port) / 判断 Origin 是否指向本机回环地址（端口不限）.
func isLocalOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
