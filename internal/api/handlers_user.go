package api

import (
	"encoding/json"
	"net/http"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/util"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
    users, err := s.st.ListUsers()
    if err != nil {
        writeAppError(w, http.StatusInternalServerError, ErrUserListFailed, "Failed to list users")
        return
    }
    // 不返回密码 hash
    type UserDTO struct {
        ID         string `json:"id"`
        Username   string `json:"username"`
        Role       string `json:"role"`
        Disabled   bool   `json:"disabled"`
        MustChange bool   `json:"mustChange"`
        CreatedAt  string `json:"createdAt"`
    }
    dtos := make([]UserDTO, 0, len(users))
    for _, u := range users {
        dtos = append(dtos, UserDTO{
            ID:         u.ID,
            Username:   u.Username,
            Role:       string(u.Role),
            Disabled:   u.Disabled,
            MustChange: u.MustChange,
            CreatedAt:  u.CreatedAt.Format("2006-01-02 15:04:05"),
        })
    }
    writeJSON(w, http.StatusOK, map[string]any{"users": dtos})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Username string `json:"username"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
        writeAppError(w, http.StatusBadRequest, ErrUserInvalidRequest, "Username is required")
        return
    }
    // 检查用户名是否已存在
    existing, _ := s.st.GetUserByUsername(req.Username)
    if existing != nil {
        writeAppError(w, http.StatusConflict, ErrUserAlreadyExists, "Username already exists",
    		map[string]interface{}{"username": req.Username})
        return
    }
    password := util.GenerateRandomPassword()
    user, err := s.st.CreateUser(req.Username, password, store.RoleUser, true)
    if err != nil {
        writeAppError(w, http.StatusInternalServerError, ErrUserSaveFailed, "Failed to create user")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{
        "id":           user.ID,
        "username":     user.Username,
        "tempPassword": password, // 只在创建时返回一次
    })
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
    session := r.Context().Value(sessionKey).(*auth.SessionData)
    username := r.PathValue("username")
    if username == "admin" && session.Username == "admin" {
        // 防止管理员禁用自己
    }
    user, err := s.st.GetUserByUsername(username)
    if err != nil || user == nil {
        writeAppError(w, http.StatusNotFound, ErrUserNotFound, "User not found",
    		map[string]interface{}{"username": username})
        return
    }
    var req struct {
        Disabled      *bool  `json:"disabled"`
        ResetPassword bool   `json:"resetPassword"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeAppError(w, http.StatusBadRequest, ErrUserInvalidRequest, "Invalid request format")
        return
    }
    resp := map[string]any{"username": username}
    if req.Disabled != nil {
        if username == session.Username {
            writeAppError(w, http.StatusBadRequest, ErrUserCannotSelfDisable, "Cannot disable your own account")
            return
        }
        user.Disabled = *req.Disabled
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
    writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
    session := r.Context().Value(sessionKey).(*auth.SessionData)
    username := r.PathValue("username")
    if username == session.Username {
        writeAppError(w, http.StatusBadRequest, ErrUserCannotSelfDelete, "Cannot delete your own account")
        return
    }
    if err := s.st.DeleteUser(username); err != nil {
        writeAppError(w, http.StatusInternalServerError, ErrUserDeleteFailed, "Failed to delete user")
        return
    }
    writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
