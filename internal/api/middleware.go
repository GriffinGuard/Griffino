package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/GriffinGuard/Griffino/internal/auth"
)

type contextKey string
const sessionKey contextKey = "session"

func (s *Server) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 提取 token
        authHeader := r.Header.Get("Authorization")
        if !strings.HasPrefix(authHeader, "Bearer ") {
            writeAppError(w, http.StatusUnauthorized, ErrAuthNotLoggedIn, "Not logged in")
            return
        }
        token := strings.TrimPrefix(authHeader, "Bearer ")

        // 验证 session
        session, err := s.sessionMgr.Get(r.Context(), token)
        if err != nil || session == nil {
            writeAppError(w, http.StatusUnauthorized, ErrAuthTokenInvalid, "Token is invalid or expired")
            return
        }

        // 续期
        _ = s.sessionMgr.Refresh(r.Context(), token)

        // 注入到 context
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
        w.Header().Set("Access-Control-Allow-Origin", "*")
        w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
        w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

        // 预检请求直接返回
        if r.Method == http.MethodOptions {
            w.WriteHeader(http.StatusNoContent)
            return
        }

        next.ServeHTTP(w, r)
    })
}