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
)

// ErrorCode 机器可读的错误码，前端根据此字段决定展示内容和操作引导
type ErrorCode string

const (
	// 插件相关
	ErrPluginNotFound        ErrorCode = "PLUGIN_NOT_FOUND"
	ErrPluginAlreadyInstalled ErrorCode = "PLUGIN_ALREADY_INSTALLED"
	ErrPluginAlreadyRunning  ErrorCode = "PLUGIN_ALREADY_RUNNING"
	ErrPluginNotRunning      ErrorCode = "PLUGIN_NOT_RUNNING"
	ErrPluginNotConfigured   ErrorCode = "PLUGIN_NOT_CONFIGURED"
	ErrPluginInvalidState    ErrorCode = "PLUGIN_INVALID_STATE"
	ErrPluginStartFailed     ErrorCode = "PLUGIN_START_FAILED"
	ErrPluginStopFailed      ErrorCode = "PLUGIN_STOP_FAILED"
	ErrPluginUninstallFailed ErrorCode = "PLUGIN_UNINSTALL_FAILED"
	ErrPluginLoadFailed      ErrorCode = "PLUGIN_LOAD_FAILED"
	ErrPluginSaveFailed      ErrorCode = "PLUGIN_SAVE_FAILED"
	ErrPluginImageNotAllowed ErrorCode = "PLUGIN_IMAGE_NOT_ALLOWED"
	ErrPluginListFailed      ErrorCode = "PLUGIN_LIST_FAILED"

	// Registry 相关
	ErrRegistryFetchFailed   ErrorCode = "REGISTRY_FETCH_FAILED"
	ErrRegistryDownloadFailed ErrorCode = "REGISTRY_DOWNLOAD_FAILED"

	// 认证相关
	ErrAuthInvalidRequest    ErrorCode = "AUTH_INVALID_REQUEST"
	ErrAuthInvalidCredentials ErrorCode = "AUTH_INVALID_CREDENTIALS"
	ErrAuthTokenInvalid      ErrorCode = "AUTH_TOKEN_INVALID"
	ErrAuthNotLoggedIn       ErrorCode = "AUTH_NOT_LOGGED_IN"
	ErrAuthPermissionDenied  ErrorCode = "AUTH_PERMISSION_DENIED"
	ErrAuthRateLimited       ErrorCode = "AUTH_RATE_LIMITED"
	ErrAuthSessionFailed     ErrorCode = "AUTH_SESSION_FAILED"
	ErrAuthPasswordTooShort  ErrorCode = "AUTH_PASSWORD_TOO_SHORT"
	ErrAuthWrongPassword     ErrorCode = "AUTH_WRONG_PASSWORD"
	ErrAuthPasswordHashFailed ErrorCode = "AUTH_PASSWORD_HASH_FAILED"
	ErrAuthPasswordSaveFailed ErrorCode = "AUTH_PASSWORD_SAVE_FAILED"

	// 用户管理相关
	ErrUserNotFound          ErrorCode = "USER_NOT_FOUND"
	ErrUserAlreadyExists     ErrorCode = "USER_ALREADY_EXISTS"
	ErrUserInvalidRequest    ErrorCode = "USER_INVALID_REQUEST"
	ErrUserCannotSelfDisable ErrorCode = "USER_CANNOT_SELF_DISABLE"
	ErrUserCannotSelfDelete  ErrorCode = "USER_CANNOT_SELF_DELETE"
	ErrUserSaveFailed        ErrorCode = "USER_SAVE_FAILED"
	ErrUserDeleteFailed      ErrorCode = "USER_DELETE_FAILED"
	ErrUserListFailed        ErrorCode = "USER_LIST_FAILED"

	// 路由相关
	ErrRouteInvalidRequest   ErrorCode = "ROUTE_INVALID_REQUEST"
	ErrRouteFetchFailed      ErrorCode = "ROUTE_FETCH_FAILED"
	ErrRouteSaveFailed       ErrorCode = "ROUTE_SAVE_FAILED"

	// 蓝图相关
	ErrBlueprintNotFound      ErrorCode = "BLUEPRINT_NOT_FOUND"
	ErrBlueprintInvalidRequest ErrorCode = "BLUEPRINT_INVALID_REQUEST"
	ErrBlueprintSaveFailed    ErrorCode = "BLUEPRINT_SAVE_FAILED"
	ErrBlueprintDeleteFailed  ErrorCode = "BLUEPRINT_DELETE_FAILED"
	ErrBlueprintFetchFailed   ErrorCode = "BLUEPRINT_FETCH_FAILED"
	
	ErrActionNotFound   ErrorCode = "ACTION_NOT_FOUND"
	ErrActionRateLimited ErrorCode = "ACTION_RATE_LIMITED"
	ErrActionSendFailed  ErrorCode = "ACTION_SEND_FAILED"

	// 用户配置相关
	ErrUserConfigInvalidRequest ErrorCode = "USER_CONFIG_INVALID_REQUEST"
	ErrUserConfigFetchFailed    ErrorCode = "USER_CONFIG_FETCH_FAILED"
	ErrUserConfigSaveFailed     ErrorCode = "USER_CONFIG_SAVE_FAILED"

	// 系统相关
	ErrSystemNotInitialized  ErrorCode = "SYSTEM_NOT_INITIALIZED"
	ErrSystemInternal        ErrorCode = "SYSTEM_INTERNAL_ERROR"

	ErrPluginLogNotFound ErrorCode = "PLUGIN_LOG_NOT_FOUND"

	ErrStatusViewNotFound   ErrorCode = "STATUS_VIEW_NOT_FOUND"
	ErrStatusViewFetchFailed ErrorCode = "STATUS_VIEW_FETCH_FAILED"
)

// AppError 统一错误响应结构
type AppError struct {
	Code    ErrorCode   `json:"code"`
	Message string      `json:"message"`
	Detail  interface{} `json:"detail,omitempty"`
}

// writeAppError 替换原来的 writeError，返回结构化错误
func writeAppError(w http.ResponseWriter, status int, code ErrorCode, message string, detail ...interface{}) {
	var d interface{}
	if len(detail) > 0 {
		d = detail[0]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": AppError{
			Code:    code,
			Message: message,
			Detail:  d,
		},
	})
}