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
	"github.com/GriffinGuard/Griffino/internal/store"
	"time"
)

// PluginDTO is the public plugin info DTO, with sensitive fields stripped / 对外暴露的插件信息，过滤敏感字段.
type PluginDTO struct {
	ID          string             `json:"id"`
	PluginDir   string             `json:"pluginDir"`
	Status      store.PluginStatus `json:"status"`
	InstalledAt time.Time          `json:"installedAt"`
	IsDevPlugin bool               `json:"isDevPlugin,omitempty"`
	ConfigDirty bool               `json:"configDirty,omitempty"`
	FailStage   string             `json:"failStage,omitempty"`
	FailReason  string             `json:"failReason,omitempty"`
	RuntimeInfo *RuntimeInfoDTO    `json:"runtimeInfo,omitempty"`
}

// RuntimeInfoDTO strips password fields / 过滤掉密码字段.
type RuntimeInfoDTO struct {
	Containers   map[string]string `json:"containers"`
	Network      string            `json:"network"`
	RabbitMQUser string            `json:"rabbitmqUser"`
	RedisUser    string            `json:"redisUser"`
}

func toPluginDTO(p *store.PluginInstance) PluginDTO {
	dto := PluginDTO{
		ID:          p.ID,
		PluginDir:   p.PluginDir,
		Status:      p.Status,
		InstalledAt: p.InstalledAt,
		IsDevPlugin: p.IsDevPlugin,
		ConfigDirty: p.ConfigDirty,
		FailStage:   p.FailStage,
		FailReason:  p.FailReason,
	}
	if p.RuntimeInfo != nil {
		dto.RuntimeInfo = &RuntimeInfoDTO{
			Containers:   p.RuntimeInfo.Containers,
			Network:      p.RuntimeInfo.Network,
			RabbitMQUser: p.RuntimeInfo.RabbitMQUser,
			RedisUser:    p.RuntimeInfo.RedisUser,
		}
	}
	return dto
}

func toPluginDTOs(plugins []*store.PluginInstance) []PluginDTO {
	dtos := make([]PluginDTO, len(plugins))
	for i, p := range plugins {
		dtos[i] = toPluginDTO(p)
	}
	return dtos
}
