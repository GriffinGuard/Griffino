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

// ErrorCode is a machine-readable error code the frontend uses to decide what to
// show and how to guide the user / 机器可读错误码，前端据此决定展示与操作引导.
type ErrorCode string

const (
	// Plugin / 插件相关
	ErrPluginNotFound         ErrorCode = "PLUGIN_NOT_FOUND"
	ErrPluginAlreadyInstalled ErrorCode = "PLUGIN_ALREADY_INSTALLED"
	ErrPluginAlreadyRunning   ErrorCode = "PLUGIN_ALREADY_RUNNING"
	ErrPluginNotRunning       ErrorCode = "PLUGIN_NOT_RUNNING"
	ErrPluginNotConfigured    ErrorCode = "PLUGIN_NOT_CONFIGURED"
	ErrPluginInvalidState     ErrorCode = "PLUGIN_INVALID_STATE"
	ErrPluginStartFailed      ErrorCode = "PLUGIN_START_FAILED"
	ErrPluginStopFailed       ErrorCode = "PLUGIN_STOP_FAILED"
	ErrPluginUninstallFailed  ErrorCode = "PLUGIN_UNINSTALL_FAILED"
	ErrPluginLoadFailed       ErrorCode = "PLUGIN_LOAD_FAILED"
	ErrPluginSaveFailed       ErrorCode = "PLUGIN_SAVE_FAILED"
	ErrPluginImageNotAllowed  ErrorCode = "PLUGIN_IMAGE_NOT_ALLOWED"
	ErrPluginListFailed       ErrorCode = "PLUGIN_LIST_FAILED"

	// Registry / Registry 相关
	ErrRegistryFetchFailed     ErrorCode = "REGISTRY_FETCH_FAILED"
	ErrRegistryDownloadFailed  ErrorCode = "REGISTRY_DOWNLOAD_FAILED"
	ErrRegistryVersionNotFound ErrorCode = "REGISTRY_VERSION_NOT_FOUND"
	ErrPluginAlreadyUpToDate   ErrorCode = "PLUGIN_ALREADY_UP_TO_DATE"
	ErrPluginUpgradeFailed     ErrorCode = "PLUGIN_UPGRADE_FAILED"

	// Auth / 认证相关
	ErrAuthInvalidRequest     ErrorCode = "AUTH_INVALID_REQUEST"
	ErrAuthInvalidCredentials ErrorCode = "AUTH_INVALID_CREDENTIALS"
	ErrAuthTokenInvalid       ErrorCode = "AUTH_TOKEN_INVALID"
	ErrAuthNotLoggedIn        ErrorCode = "AUTH_NOT_LOGGED_IN"
	ErrAuthPermissionDenied   ErrorCode = "AUTH_PERMISSION_DENIED"
	ErrAuthRateLimited        ErrorCode = "AUTH_RATE_LIMITED"
	ErrAuthSessionFailed      ErrorCode = "AUTH_SESSION_FAILED"
	ErrAuthPasswordTooShort   ErrorCode = "AUTH_PASSWORD_TOO_SHORT"
	ErrAuthWrongPassword      ErrorCode = "AUTH_WRONG_PASSWORD"
	ErrAuthPasswordHashFailed ErrorCode = "AUTH_PASSWORD_HASH_FAILED"
	ErrAuthPasswordSaveFailed ErrorCode = "AUTH_PASSWORD_SAVE_FAILED"

	// User management / 用户管理相关
	ErrUserNotFound          ErrorCode = "USER_NOT_FOUND"
	ErrUserAlreadyExists     ErrorCode = "USER_ALREADY_EXISTS"
	ErrUserInvalidRequest    ErrorCode = "USER_INVALID_REQUEST"
	ErrUserCannotSelfDisable ErrorCode = "USER_CANNOT_SELF_DISABLE"
	ErrUserCannotSelfDelete  ErrorCode = "USER_CANNOT_SELF_DELETE"
	ErrUserSaveFailed        ErrorCode = "USER_SAVE_FAILED"
	ErrUserDeleteFailed      ErrorCode = "USER_DELETE_FAILED"
	ErrUserListFailed        ErrorCode = "USER_LIST_FAILED"

	// Routing / 路由相关
	ErrRouteInvalidRequest ErrorCode = "ROUTE_INVALID_REQUEST"
	ErrRouteFetchFailed    ErrorCode = "ROUTE_FETCH_FAILED"
	ErrRouteSaveFailed     ErrorCode = "ROUTE_SAVE_FAILED"

	// Blueprint / 蓝图相关
	ErrBlueprintNotFound       ErrorCode = "BLUEPRINT_NOT_FOUND"
	ErrBlueprintInvalidRequest ErrorCode = "BLUEPRINT_INVALID_REQUEST"
	ErrBlueprintSaveFailed     ErrorCode = "BLUEPRINT_SAVE_FAILED"
	ErrBlueprintDeleteFailed   ErrorCode = "BLUEPRINT_DELETE_FAILED"
	ErrBlueprintFetchFailed    ErrorCode = "BLUEPRINT_FETCH_FAILED"
	ErrBlueprintPortMismatch   ErrorCode = "BLUEPRINT_PORT_MISMATCH"

	ErrActionNotFound       ErrorCode = "ACTION_NOT_FOUND"
	ErrActionInvalidRequest ErrorCode = "ACTION_INVALID_REQUEST"
	ErrActionRateLimited    ErrorCode = "ACTION_RATE_LIMITED"
	ErrActionSendFailed     ErrorCode = "ACTION_SEND_FAILED"

	// User config / 用户配置相关
	ErrUserConfigInvalidRequest ErrorCode = "USER_CONFIG_INVALID_REQUEST"
	ErrUserConfigFetchFailed    ErrorCode = "USER_CONFIG_FETCH_FAILED"
	ErrUserConfigSaveFailed     ErrorCode = "USER_CONFIG_SAVE_FAILED"

	// System / 系统相关
	ErrSystemNotInitialized ErrorCode = "SYSTEM_NOT_INITIALIZED"
	ErrSystemInternal       ErrorCode = "SYSTEM_INTERNAL_ERROR"

	ErrPluginLogNotFound ErrorCode = "PLUGIN_LOG_NOT_FOUND"

	// Component / dashboard widgets (unified concept) / 组件、仪表盘相关
	ErrWidgetNotFound          ErrorCode = "WIDGET_NOT_FOUND"
	ErrWidgetDataFailed        ErrorCode = "WIDGET_DATA_FAILED"
	ErrDashboardInvalidRequest ErrorCode = "DASHBOARD_INVALID_REQUEST"
	ErrDashboardSaveFailed     ErrorCode = "DASHBOARD_SAVE_FAILED"

	// Task (workflow run instances) / 任务（Workflow 运行实例）相关
	ErrTaskNotFound    ErrorCode = "TASK_NOT_FOUND"
	ErrTaskFetchFailed ErrorCode = "TASK_FETCH_FAILED"

	// Settings / 设置相关
	ErrSettingsInvalidRequest ErrorCode = "SETTINGS_INVALID_REQUEST"
	ErrSettingsSaveFailed     ErrorCode = "SETTINGS_SAVE_FAILED"
	ErrSettingsFetchFailed    ErrorCode = "SETTINGS_FETCH_FAILED"

	// Session management / 会话管理相关
	ErrSessionNotFound   ErrorCode = "SESSION_NOT_FOUND"
	ErrSessionListFailed ErrorCode = "SESSION_LIST_FAILED"

	// User management (precise codes) / 用户管理精准错误码
	ErrUsernameTaken         ErrorCode = "USERNAME_TAKEN"
	ErrUsernameInvalid       ErrorCode = "USERNAME_INVALID"
	ErrUserAlreadyDisabled   ErrorCode = "USER_ALREADY_DISABLED"
	ErrUserAlreadyEnabled    ErrorCode = "USER_ALREADY_ENABLED"
	ErrCannotDisableSelf     ErrorCode = "CANNOT_DISABLE_SELF"
	ErrCannotDeleteLastAdmin ErrorCode = "CANNOT_DELETE_LAST_ADMIN"
	ErrCannotDemoteLastAdmin ErrorCode = "CANNOT_DEMOTE_LAST_ADMIN"

	// Auth (precise codes) / 认证精准错误码
	ErrAccountDisabled        ErrorCode = "ACCOUNT_DISABLED"
	ErrPasswordReuseForbidden ErrorCode = "PASSWORD_REUSE_FORBIDDEN"

	// Email / 邮箱相关
	ErrEmailInvalid         ErrorCode = "EMAIL_INVALID"
	ErrEmailAlreadyVerified ErrorCode = "EMAIL_ALREADY_VERIFIED"

	// Setup wizard / 安装向导相关
	ErrSetupAlreadyCompleted ErrorCode = "SETUP_ALREADY_COMPLETED"
	ErrSetupNotCompleted     ErrorCode = "SETUP_NOT_COMPLETED"
	ErrLicenseNotAccepted    ErrorCode = "LICENSE_NOT_ACCEPTED"
	ErrAdminAlreadyExists    ErrorCode = "ADMIN_ALREADY_EXISTS"

	// SMTP / 邮件服务相关
	ErrSMTPConnectionFailed ErrorCode = "SMTP_CONNECTION_FAILED"
	ErrSMTPAuthFailed       ErrorCode = "SMTP_AUTH_FAILED"
	ErrSMTPSendFailed       ErrorCode = "SMTP_SEND_FAILED"

	// System config / 系统配置相关
	ErrConfigKeyInvalid   ErrorCode = "CONFIG_KEY_INVALID"
	ErrConfigValueInvalid ErrorCode = "CONFIG_VALUE_INVALID"
)

// AppError is the unified error response shape / 统一错误响应结构.
type AppError struct {
	Code    ErrorCode   `json:"code"`
	Message string      `json:"message"`
	Detail  interface{} `json:"detail,omitempty"`
}

// writeAppError writes a structured error response / 返回结构化错误响应.
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
