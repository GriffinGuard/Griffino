package api

import (
	"github.com/GriffinGuard/Griffino/internal/store"
	"time"
)

// PluginDTO 对外暴露的插件信息，过滤敏感字段
type PluginDTO struct {
	ID          string           `json:"id"`
	PluginDir   string           `json:"pluginDir"`
	Status      store.PluginStatus `json:"status"`
	InstalledAt time.Time        `json:"installedAt"`
	IsDevPlugin bool             `json:"isDevPlugin,omitempty"`
	ConfigDirty bool             `json:"configDirty,omitempty"`
	FailStage   string           `json:"failStage,omitempty"`
	FailReason  string           `json:"failReason,omitempty"`
	RuntimeInfo *RuntimeInfoDTO  `json:"runtimeInfo,omitempty"`
}

// RuntimeInfoDTO 过滤掉密码字段
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