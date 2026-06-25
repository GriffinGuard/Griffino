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

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/util"
	"golang.org/x/crypto/bcrypt"
)

// handleListUsers lists all user accounts.
//
//	@Summary	List users
//	@Tags		Users
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	map[string]interface{}
//	@Failure	500	{object}	api.AppError
//	@Router		/users [get]
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers()
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserListFailed, "Failed to list users")
		return
	}
	// Don't return the password hash / 不返回密码 hash
	type UserDTO struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
		Role        string `json:"role"`
		Disabled    bool   `json:"disabled"`
		MustChange  bool   `json:"mustChange"`
		CreatedAt   string `json:"createdAt"`
	}
	dtos := make([]UserDTO, 0, len(users))
	for _, u := range users {
		dtos = append(dtos, UserDTO{
			ID:          u.ID,
			Username:    u.Username,
			Email:       u.Email,
			DisplayName: u.DisplayName,
			Role:        string(u.Role),
			Disabled:    u.Disabled,
			MustChange:  u.MustChange,
			CreatedAt:   u.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": dtos})
}

// countAdmins returns the number of non-disabled admin users / 返回未禁用的管理员数量
func (s *Server) countAdmins() (int, error) {
	users, err := s.st.ListUsers()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, u := range users {
		if u.Role == store.RoleAdmin && !u.Disabled {
			count++
		}
	}
	return count, nil
}

// handleCreateUser creates a new user with a generated temporary password.
//
//	@Summary	Create a user
//	@Tags		Users
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		body	body		object	true	"username"
//	@Success	200		{object}	map[string]interface{}
//	@Failure	400		{object}	api.AppError
//	@Failure	409		{object}	api.AppError
//	@Failure	500		{object}	api.AppError
//	@Router		/users [post]
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		writeAppError(w, http.StatusBadRequest, ErrUserInvalidRequest, "Username is required")
		return
	}
	// Check if username already exists / 检查用户名是否已存在
	existing, _ := s.st.GetUserByUsername(req.Username)
	if existing != nil {
		writeAppError(w, http.StatusConflict, ErrUsernameTaken, "Username already exists",
			map[string]interface{}{"username": req.Username})
		return
	}
	password := util.GenerateRandomPassword()
	user, err := s.st.CreateUser(req.Username, password, store.RoleUser, true)
	if err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserSaveFailed, "Failed to create user")
		return
	}
	s.writeAuditLog(r, "CREATE_USER", "users/"+user.Username, "Created user "+user.Username, "info")
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           user.ID,
		"username":     user.Username,
		"tempPassword": password, // Returned only once during creation / 只在创建时返回一次
	})
}

// handleUpdateUser enables/disables a user or resets their password.
//
//	@Summary	Update a user
//	@Tags		Users
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		username	path		string	true	"Username"
//	@Param		body		body		object	true	"disabled and resetPassword flags"
//	@Success	200			{object}	map[string]interface{}
//	@Failure	400			{object}	api.AppError
//	@Failure	404			{object}	api.AppError
//	@Failure	500			{object}	api.AppError
//	@Router		/users/{username} [patch]
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	username := r.PathValue("username")
	if username == "admin" && session.Username == "admin" {
		// Prevent admin from disabling themselves / 防止管理员禁用自己
	}
	user, err := s.st.GetUserByUsername(username)
	if err != nil || user == nil {
		writeAppError(w, http.StatusNotFound, ErrUserNotFound, "User not found",
			map[string]interface{}{"username": username})
		return
	}
	var req struct {
		Disabled      *bool   `json:"disabled"`
		ResetPassword bool    `json:"resetPassword"`
		Role          *string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrUserInvalidRequest, "Invalid request format")
		return
	}
	resp := map[string]any{"username": username}
	if req.Disabled != nil {
		if username == session.Username {
			writeAppError(w, http.StatusForbidden, ErrCannotDisableSelf, "Cannot disable your own account")
			return
		}
		// Prevent locking out the last active admin / 避免锁定最后一个活跃管理员
		if *req.Disabled && user.Role == store.RoleAdmin {
			admins, err := s.countAdmins()
			if err != nil {
				writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to check admin count")
				return
			}
			if admins <= 1 {
				writeAppError(w, http.StatusForbidden, ErrCannotDeleteLastAdmin, "Cannot disable the last admin account")
				return
			}
		}
		user.Disabled = *req.Disabled
	}
	if req.Role != nil {
		newRole := store.UserRole(*req.Role)
		if newRole != store.RoleAdmin && newRole != store.RoleUser {
			writeAppError(w, http.StatusBadRequest, ErrUsernameInvalid, "Role must be 'admin' or 'user'")
			return
		}
		if username == session.Username {
			writeAppError(w, http.StatusForbidden, ErrCannotDemoteLastAdmin, "Cannot change your own role")
			return
		}
		// Prevent demoting the last admin / 避免降级最后一个管理员
		if newRole == store.RoleUser && user.Role == store.RoleAdmin {
			admins, err := s.countAdmins()
			if err != nil {
				writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to check admin count")
				return
			}
			if admins <= 1 {
				writeAppError(w, http.StatusForbidden, ErrCannotDemoteLastAdmin, "Cannot demote the last admin account")
				return
			}
		}
		user.Role = newRole
	}
	if req.ResetPassword {
		newPass := util.GenerateRandomPassword()
		hash, _ := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
		user.PasswordHash = string(hash)
		user.MustChange = true
		resp["tempPassword"] = newPass
	}
	if err := s.st.UpdateUser(user); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserSaveFailed, "Failed to update user")
		return
	}
	// Audit log — record each type of change separately / 审计日志：分类记录每种变更
	resource := "users/" + username
	if req.Disabled != nil {
		action := "ENABLE_USER"
		if *req.Disabled {
			action = "DISABLE_USER"
		}
		s.writeAuditLog(r, action, resource, "", "info")
	}
	if req.Role != nil {
		s.writeAuditLog(r, "CHANGE_USER_ROLE", resource, "role="+*req.Role, "info")
	}
	if req.ResetPassword {
		s.writeAuditLog(r, "RESET_USER_PASSWORD", resource, "", "warning")
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleUpdateUserProfile updates a user's email and display name.
//
//	@Summary	Update a user profile
//	@Tags		Users
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		username	path		string	true	"Username"
//	@Param		body		body		object	true	"email and displayName"
//	@Success	200			{object}	map[string]interface{}
//	@Failure	400			{object}	api.AppError
//	@Failure	404			{object}	api.AppError
//	@Failure	500			{object}	api.AppError
//	@Router		/users/{username}/profile [patch]
func (s *Server) handleUpdateUserProfile(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	user, err := s.st.GetUserByUsername(username)
	if err != nil || user == nil {
		writeAppError(w, http.StatusNotFound, ErrUserNotFound, "User not found",
			map[string]interface{}{"username": username})
		return
	}
	var req struct {
		Email       *string `json:"email"`
		DisplayName *string `json:"displayName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAppError(w, http.StatusBadRequest, ErrAuthInvalidRequest, "Invalid request format")
		return
	}
	if req.Email != nil {
		user.Email = *req.Email
	}
	if req.DisplayName != nil {
		user.DisplayName = *req.DisplayName
	}
	if err := s.st.UpdateUser(user); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserSaveFailed, "Failed to update user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          user.ID,
		"username":    user.Username,
		"email":       user.Email,
		"displayName": user.DisplayName,
		"role":        string(user.Role),
		"disabled":    user.Disabled,
		"mustChange":  user.MustChange,
		"createdAt":   user.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

// handleDeleteUser deletes a user account.
//
//	@Summary	Delete a user
//	@Tags		Users
//	@Produce	json
//	@Security	BearerAuth
//	@Param		username	path		string	true	"Username"
//	@Success	200			{object}	map[string]interface{}
//	@Failure	400			{object}	api.AppError
//	@Failure	500			{object}	api.AppError
//	@Router		/users/{username} [delete]
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey).(*auth.SessionData)
	username := r.PathValue("username")
	if username == session.Username {
		writeAppError(w, http.StatusBadRequest, ErrUserCannotSelfDelete, "Cannot delete your own account")
		return
	}
	target, err := s.st.GetUserByUsername(username)
	if err != nil || target == nil {
		writeAppError(w, http.StatusNotFound, ErrUserNotFound, "User not found",
			map[string]interface{}{"username": username})
		return
	}
	// Prevent deleting the last admin / 避免删除最后一个管理员
	if target.Role == store.RoleAdmin {
		admins, err := s.countAdmins()
		if err != nil {
			writeAppError(w, http.StatusInternalServerError, ErrSystemInternal, "Failed to check admin count")
			return
		}
		if admins <= 1 {
			writeAppError(w, http.StatusForbidden, ErrCannotDeleteLastAdmin, "Cannot delete the last admin account")
			return
		}
	}
	if err := s.st.DeleteUser(username); err != nil {
		writeAppError(w, http.StatusInternalServerError, ErrUserDeleteFailed, "Failed to delete user")
		return
	}
	s.writeAuditLog(r, "DELETE_USER", "users/"+username, "", "warning")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
