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

package progress

// LogLevel 日志级别
type LogLevel int

const (
	LevelInfo LogLevel = iota
	LevelSuccess
	LevelWarn
	LevelError
)

// 无状态日志消息
type MsgLog struct {
	PluginID string
	Level    LogLevel
	Text     string
}

// 进度条消息
type MsgPullStart struct {
	PluginID  string
	ServiceID string
	ImageRef  string
}

type MsgPullLayerUpdate struct {
	PluginID  string
	ServiceID string
	LayerID   string
	Current   int64
	Total     int64
}

type MsgPullLayerDone struct {
	PluginID  string
	ServiceID string
	LayerID   string
}

type MsgPullDone struct {
	PluginID  string
	ServiceID string
}