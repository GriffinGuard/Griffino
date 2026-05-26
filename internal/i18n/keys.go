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

package i18n

// MessageID 所有 i18n 消息的 key 常量
// 按模块分组，避免 key 拼写错误
type MessageID = string

const (
	// --- Daemon 启动 ---
	MsgDaemonStarting        MessageID = "DaemonStarting"
	MsgDaemonStarted         MessageID = "DaemonStarted"
	MsgDaemonStopping        MessageID = "DaemonStopping"
	MsgDaemonStopped         MessageID = "DaemonStopped"
	MsgDaemonAdminCreated    MessageID = "DaemonAdminCreated"
	MsgDaemonWaitingServices MessageID = "DaemonWaitingServices"
	MsgDaemonRabbitMQReady   MessageID = "DaemonRabbitMQReady"

	// --- 插件生命周期 ---
	MsgPluginInstalling      MessageID = "PluginInstalling"
	MsgPluginInstalled       MessageID = "PluginInstalled"
	MsgPluginStarting        MessageID = "PluginStarting"
	MsgPluginPulling         MessageID = "PluginPulling"
	MsgPluginStarted         MessageID = "PluginStarted"
	MsgPluginStopping        MessageID = "PluginStopping"
	MsgPluginStopped         MessageID = "PluginStopped"
	MsgPluginUninstalling    MessageID = "PluginUninstalling"
	MsgPluginUninstalled     MessageID = "PluginUninstalled"
	MsgPluginRestoring       MessageID = "PluginRestoring"
	MsgPluginRestored        MessageID = "PluginRestored"
	MsgPluginRestoreFailed   MessageID = "PluginRestoreFailed"
	MsgPluginInterrupted     MessageID = "PluginInterrupted"

	// --- 插件状态显示 ---
	MsgStatusPendingSetup MessageID = "StatusPendingSetup"
	MsgStatusReady        MessageID = "StatusReady"
	MsgStatusPulling      MessageID = "StatusPulling"
	MsgStatusStarting     MessageID = "StatusStarting"
	MsgStatusRunning      MessageID = "StatusRunning"
	MsgStatusStopped      MessageID = "StatusStopped"
	MsgStatusFailed       MessageID = "StatusFailed"

	// --- CLI 操作反馈 ---
	MsgOperationSuccess  MessageID = "OperationSuccess"
	MsgOperationFailed   MessageID = "OperationFailed"
	MsgNotRunningError   MessageID = "NotRunningError"

	// --- Dev 插件 ---
	MsgDevPluginInstalled   MessageID = "DevPluginInstalled"
	MsgDevPluginUninstalled MessageID = "DevPluginUninstalled"
	MsgDevPluginStarted     MessageID = "DevPluginStarted"
	MsgDevPluginStopped     MessageID = "DevPluginStopped"

	// --- 系统 ---
	MsgSystemRabbitMQ MessageID = "SystemRabbitMQ"
	MsgSystemRedis    MessageID = "SystemRedis"

	// --- install ---
	MsgInstallLoadingManifest  MessageID = "InstallLoadingManifest"
	MsgInstallCheckingAllowlist MessageID = "InstallCheckingAllowlist"
	MsgInstallSuccess          MessageID = "InstallSuccess"
	MsgInstallSuccessID        MessageID = "InstallSuccessID"
	MsgInstallSuccessName      MessageID = "InstallSuccessName"
	MsgInstallSuccessVersion   MessageID = "InstallSuccessVersion"
	MsgInstallNextStep         MessageID = "InstallNextStep"
	MsgDevInstallSuccess       MessageID = "DevInstallSuccess"
	MsgDevInstallNextStep      MessageID = "DevInstallNextStep"

	// --- start ---
	MsgStartSuccess            MessageID = "StartSuccess"
	MsgStartSuccessNetwork     MessageID = "StartSuccessNetwork"
	MsgStartSuccessMQUser      MessageID = "StartSuccessMQUser"
	MsgStartSuccessContainers  MessageID = "StartSuccessContainers"
	MsgStartSuccessContainer   MessageID = "StartSuccessContainer"
	MsgDevStartSuccess         MessageID = "DevStartSuccess"

	// --- stop ---
	MsgStopSuccess             MessageID = "StopSuccess"
	MsgDevStopSuccess          MessageID = "DevStopSuccess"

	// --- uninstall ---
	MsgDevUninstallSuccess     MessageID = "DevUninstallSuccess"
	MsgDevUninstallForceSuccess MessageID = "DevUninstallForceSuccess"

	// --- status ---
	MsgStatusNoPlugins         MessageID = "StatusNoPlugins"
	MsgStatusHeader            MessageID = "StatusHeader"
	MsgStatusHeaderSep         MessageID = "StatusHeaderSep"
	MsgStatusRuntimeInfo       MessageID = "StatusRuntimeInfo"
	MsgStatusContainer         MessageID = "StatusContainer"

	// --- config ---
	MsgConfigTitle            MessageID = "ConfigTitle"
	MsgConfigTip              MessageID = "ConfigTip"
	MsgConfigServiceHeader    MessageID = "ConfigServiceHeader"
	MsgConfigParamDesc        MessageID = "ConfigParamDesc"
	MsgConfigParamRequired    MessageID = "ConfigParamRequired"
	MsgConfigSaved            MessageID = "ConfigSaved"
	MsgConfigNextStep         MessageID = "ConfigNextStep"

	// --- admin ---
	MsgAdminPasswordReset     MessageID = "AdminPasswordReset"
	MsgAdminPasswordPrompt    MessageID = "AdminPasswordPrompt"

	// --- dev ---
	MsgDevDaemonNotRunning    MessageID = "DevDaemonNotRunning"
	MsgDevDaemonStartHint     MessageID = "DevDaemonStartHint"

	// --- 错误信息（用户可见）---
	MsgErrInvalidPath           MessageID = "ErrInvalidPath"
	MsgErrOpenDatabase          MessageID = "ErrOpenDatabase"
	MsgErrLoadManifest          MessageID = "ErrLoadManifest"
	MsgErrValidateManifest      MessageID = "ErrValidateManifest"
	MsgErrSavePlugin            MessageID = "ErrSavePlugin"
	MsgErrPluginAlreadyInstalled MessageID = "ErrPluginAlreadyInstalled"
	MsgErrPluginNotInstalled    MessageID = "ErrPluginNotInstalled"
	MsgErrInputParam            MessageID = "ErrInputParam"
	MsgErrSaveConfig            MessageID = "ErrSaveConfig"
	MsgErrParamRequired         MessageID = "ErrParamRequired"
	MsgErrAdminNotFound         MessageID = "ErrAdminNotFound"
	MsgErrPasswordTooShort      MessageID = "ErrPasswordTooShort"
	MsgErrUpdatePassword        MessageID = "ErrUpdatePassword"
	MsgErrGetCurrentUser        MessageID = "ErrGetCurrentUser"
	MsgErrRootNotAllowed        MessageID = "ErrRootNotAllowed"
	MsgErrRemoveContainer 		MessageID = "ErrRemoveContainer"
	MsgErrContainerNameEmpty 	MessageID = "ErrContainerNameEmpty"

	// Container lifecycle
	MsgContainerStartingService    = "container.starting_service"
	MsgContainerAlreadyRunning     = "container.already_running"
	MsgContainerRestarting         = "container.restarting"
	MsgContainerStopping           = "container.stopping"
	MsgContainerPullingImage       = "container.pulling_image"
	MsgContainerCheckingImage      = "container.checking_image"

	ErrContainerTopoSort           = "container.err_topo_sort"
	ErrContainerFindContainer      = "container.err_find_container"
	ErrContainerRestart            = "container.err_restart_container"
	ErrContainerNameConflict       = "container.err_name_conflict"
	ErrContainerImagePrep          = "container.err_image_prep"
	ErrContainerWhitelistCheck     = "container.err_whitelist_check"
	ErrContainerImageNotOfficial   = "container.err_image_not_official"
	ErrContainerImageNotAllowed    = "container.err_image_not_allowed"
	ErrContainerPullFailed         = "container.err_pull_failed"
	ErrContainerCreate             = "container.err_create_container"
	ErrContainerStart              = "container.err_start_container"
	ErrContainerStop               = "container.err_stop_container"
	ErrContainerRemove             = "container.err_remove_container"
	ErrContainerList               = "container.err_list_containers"
	ErrContainerServiceNotFound    = "container.err_service_not_found"

	// Plugin service
	ErrPluginNotInstalled          = "plugin.err_not_installed"
	ErrPluginIsDevPlugin           = "plugin.err_is_dev_plugin"
	ErrPluginNotDevPlugin          = "plugin.err_not_dev_plugin"
	ErrPluginAlreadyRunning        = "plugin.err_already_running"
	ErrPluginNotRunning            = "plugin.err_not_running"
	ErrPluginNotConfigured         = "plugin.err_not_configured"
	ErrPluginInvalidStateStart     = "plugin.err_invalid_state_start"
	ErrPluginInvalidStateStop      = "plugin.err_invalid_state_stop"
	ErrPluginLoadManifest          = "plugin.err_load_manifest"
	ErrPluginRabbitMQUnreachable   = "plugin.err_rabbitmq_unreachable"
	ErrPluginRabbitMQSync          = "plugin.err_rabbitmq_sync"
	ErrPluginRabbitMQProvision     = "plugin.err_rabbitmq_provision"
	ErrPluginRedisProvision        = "plugin.err_redis_provision"
	ErrPluginNetworkCreate         = "plugin.err_network_create"
	ErrPluginEnvBuild              = "plugin.err_env_build"
	ErrPluginContainerStart        = "plugin.err_container_start"
	ErrPluginContainerStop         = "plugin.err_container_stop"
	ErrPluginRuntimeSave           = "plugin.err_runtime_save"
	ErrPluginStatusUpdate          = "plugin.err_status_update"
	ErrPluginStopBeforeUninstall   = "plugin.err_stop_before_uninstall"
	ErrPluginDeleteRecord          = "plugin.err_delete_record"
	ErrPluginReadStatus            = "plugin.err_read_status"

	// System
	MsgSystemCheckingNetwork      = "system.msg_checking_network"
	MsgSystemCheckingRabbitMQ     = "system.msg_checking_rabbitmq"
	MsgSystemCheckingRedis        = "system.msg_checking_redis"
	MsgSystemAllServicesReady     = "system.msg_all_services_ready"
	MsgSystemRedisPasswordUpgrade = "system.msg_redis_password_upgrade"
	MsgSystemRedisManualDelete    = "system.msg_redis_manual_delete"
	MsgSystemConfigGenerated      = "system.msg_config_generated"
	MsgSystemRabbitMQRunning      = "system.msg_rabbitmq_running"
	MsgSystemStartingRabbitMQ     = "system.msg_starting_rabbitmq"
	MsgSystemRedisRunning         = "system.msg_redis_running"
	MsgSystemStartingRedis        = "system.msg_starting_redis"
	MsgSystemNetworkJoinFailed    = "system.msg_network_join_failed"
	MsgSystemRestartingContainer  = "system.msg_restarting_container"
	MsgSystemStoppingContainer    = "system.msg_stopping_container"
	MsgSystemStopContainerFailed  = "system.msg_stop_container_failed"
	MsgSystemContainerStopped     = "system.msg_container_stopped"

	// System errors
	ErrSystemStateNotInitialized  = "system.err_state_not_initialized"
	ErrSystemNetworkInit          = "system.err_network_init"
	ErrSystemConfigInit           = "system.err_config_init"
	ErrSystemRabbitMQStart        = "system.err_rabbitmq_start"
	ErrSystemRedisStart           = "system.err_redis_start"

	// Broker provisioner errors (调用方用，匹配 sentinel error 后展示)
	ErrBrokerGeneratePassword     = "broker.err_generate_password"
	ErrBrokerCreateUser           = "broker.err_create_user"
	ErrBrokerSetPermissions       = "broker.err_set_permissions"
	ErrBrokerDeclareQueue         = "broker.err_declare_queue"
	ErrBrokerBindQueue            = "broker.err_bind_queue"
	ErrBrokerDeleteUser           = "broker.err_delete_user"
	ErrBrokerSetUserPassword      = "broker.err_set_user_password"

	MsgAPIServerListening = "api.msg_server_listening"

	MsgDevDaemonListening = "devdaemon.msg_listening"

	MsgHealthPluginRecovered = "health.msg_plugin_recovered"

	ErrImageCheckFetchFailed   = "imagecheck.err_fetch_failed"
	ErrImageCheckParseFailed   = "imagecheck.err_parse_failed"
	ErrImageCheckUnapproved    = "imagecheck.err_unapproved"
)