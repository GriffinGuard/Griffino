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
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"golang.org/x/crypto/bcrypt"
)

const (
	// 登录安全相关配置
	loginMaxAttempts = 5
	loginWindowTTL   = 15 * time.Minute
	loginLockTTL     = 15 * time.Minute
)

func loginAttemptsKey(username string) string {
	return fmt.Sprintf("login:attempts:%s", username)
}

func loginLockKey(username string) string {
	return fmt.Sprintf("login:lock:%s", username)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrAuthInvalidRequest, "Invalid request format")
		return
	}

	ctx := r.Context()

	// 检查是否已被锁定
	lockKey := loginLockKey(req.Username)
	locked, _ := s.sessionMgr.Exists(ctx, lockKey)
	if locked {
		ttl, _ := s.sessionMgr.TTL(ctx, lockKey)
		writeAppError(w, http.StatusTooManyRequests, ErrAuthRateLimited, "Account is locked",
    		map[string]interface{}{"retryAfterMinutes": int(ttl.Minutes()) + 1})
		return
	}

	// 验证密码
	user, err := s.st.VerifyPassword(req.Username, req.Password)
	if err != nil {
		// 记录失败次数
		attemptsKey := loginAttemptsKey(req.Username)
		attempts, _ := s.sessionMgr.Incr(ctx, attemptsKey)
		if attempts == 1 {
			// 第一次失败，设置窗口 TTL
			s.sessionMgr.Expire(ctx, attemptsKey, loginWindowTTL)
		}
		remaining := loginMaxAttempts - int(attempts)
		if remaining <= 0 {
			// 达到上限，锁定账号，清除计数
			s.sessionMgr.Set(ctx, lockKey, "1", loginLockTTL)
			s.sessionMgr.Del(ctx, attemptsKey)
			writeAppError(w, http.StatusTooManyRequests, ErrAuthRateLimited, "Too many failed attempts, account locked",
    			map[string]interface{}{"lockDurationMinutes": int(loginLockTTL.Minutes())})
			return
		}
		writeAppError(w, http.StatusUnauthorized, ErrAuthInvalidCredentials, "Invalid username or password",
    		map[string]interface{}{"remainingAttempts": remaining})
		return
	}

	// 登录成功，清除失败计数
	s.sessionMgr.Del(ctx, loginAttemptsKey(req.Username))

	token, err := s.sessionMgr.Create(ctx, auth.SessionData{
		UserID:   user.ID,
		Username: user.Username,
		Role:     string(user.Role),
	})
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrAuthSessionFailed, "Failed to create session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"username":   user.Username,
		"role":       user.Role,
		"mustChange": user.MustChange,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	_ = s.sessionMgr.Delete(r.Context(), token)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	writeJSON(w, http.StatusOK, map[string]any{
		"userId":   session.UserID,
		"username": session.Username,
		"role":     session.Role,
	})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)

	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrAuthInvalidRequest, "Invalid request format")
		return
	}

	if len(req.NewPassword) < 8 {
		writeAppError(w, http.StatusBadRequest, ErrAuthPasswordTooShort, "Password must be at least 8 characters")
		return
	}

	user, err := s.st.VerifyPassword(session.Username, req.OldPassword)
	if err != nil {
		writeAppError(w, http.StatusForbidden, ErrAuthWrongPassword, "Current password is incorrect")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrAuthPasswordHashFailed, "Failed to hash password")
		return
	}

	user.PasswordHash = string(hash)
	user.MustChange = false
	if err := s.st.UpdateUser(user); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrAuthPasswordSaveFailed, "Failed to update password")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}