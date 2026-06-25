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

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/GriffinGuard/Griffino/internal/crypto"
	bolt "go.etcd.io/bbolt"
)

var bucketSystem = []byte("system")
var bucketPlugins = []byte("plugins")
var bucketBlueprints = []byte("blueprints")
var bucketSchemas = []byte("schemas")

// SystemState stores system-level state such as RabbitMQ credentials / 存储系统级状态，如 RabbitMQ 凭据等
type SystemState struct {
	RabbitMQAdminUser      string `json:"rabbitmqAdminUser"`
	RabbitMQAdminPassword  string `json:"rabbitmqAdminPassword"`
	RabbitMQPort           int    `json:"rabbitmqPort"`
	RabbitMQManagementPort int    `json:"rabbitmqManagementPort"`
	RedisPort              int    `json:"redisPort"`
	RedisPassword          string `json:"redisPassword"`         // System Redis auth password / 系统 Redis 认证密码
	RabbitMQContainerName  string `json:"rabbitmqContainerName"` // Non-empty when customised / 自定义时不为空
	RedisContainerName     string `json:"redisContainerName"`    // Non-empty when customised / 自定义时不为空
}

// PluginStatus is the current status of a plugin instance / 插件实例的当前状态
type PluginStatus string

const (
	StatusPendingSetup PluginStatus = "pending_setup" // Manifest downloaded, config not yet done / 元文件已下载，未完成配置
	StatusReady        PluginStatus = "ready"         // Config done, never started / 配置完成，从未启动
	StatusPulling      PluginStatus = "pulling"       // Pulling images / 正在拉取镜像
	StatusStarting     PluginStatus = "starting"      // Images ready, container starting / 镜像就绪，容器启动中
	StatusRunning      PluginStatus = "running"       // Running / 运行中
	StatusStopped      PluginStatus = "stopped"       // Stopped / 已停止
	StatusFailed       PluginStatus = "failed"        // Start failed / 启动失败
)

// PluginInstance holds the complete state of a single plugin instance / 单个插件实例的完整状态
type PluginInstance struct {
	ID          string                       `json:"id"`
	PluginDir   string                       `json:"pluginDir"`
	Status      PluginStatus                 `json:"status"`
	InstalledAt time.Time                    `json:"installedAt"`
	IsDevPlugin bool                         `json:"isDevPlugin,omitempty"`
	AdminConfig map[string]map[string]string `json:"adminConfig,omitempty"`
	RuntimeInfo *RuntimeInfo                 `json:"runtimeInfo,omitempty"`
	ConfigDirty bool                         `json:"configDirty,omitempty"`
	FailReason  string                       `json:"failReason,omitempty"`
	FailStage   string                       `json:"failStage,omitempty"` // "pull" | "start"
}

// RuntimeInfo holds runtime information after a plugin has started / 插件启动后的运行时信息
type RuntimeInfo struct {
	Containers       map[string]string `json:"containers"` // map[serviceId]containerName
	Network          string            `json:"network"`
	RabbitMQUser     string            `json:"rabbitmqUser"`
	RabbitMQPassword string            `json:"rabbitmqPassword"`
	RedisUser        string            `json:"redisUser"`
	RedisPassword    string            `json:"redisPassword"`
}

// Store handles state read/write / 负责状态的读写
type Store struct {
	db     *bolt.DB
	cipher *crypto.Cipher
}

// DB exposes the underlying bolt.DB for subsystems that need direct BoltDB access / 暴露底层 bolt.DB，供需要直接操作 BoltDB 的子系统使用
func (s *Store) DB() *bolt.DB {
	return s.db
}

// New initializes the Store; dbPath is the database file path (e.g. ~/.griffino/griffino.db) / 初始化 Store，dbPath 是数据库文件路径
func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// At-rest encryption master key lives in the same directory as the DB (~/.griffino/secret.key, mode 0600) / at-rest 加密主密钥与数据库同目录
	cph, err := crypto.NewFromKeyFile(filepath.Join(filepath.Dir(dbPath), "secret.key"))
	if err != nil {
		return nil, fmt.Errorf("failed to init secret cipher: %w", err)
	}

	db, err := bolt.Open(dbPath, 0644, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Ensure buckets exist / 确保 bucket 存在
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketPlugins); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSystem); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketBlueprints); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSchemas); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSettings); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(bucketAuditLogs)
		return err
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize database buckets: %w", err)
	}

	return &Store{db: db, cipher: cph}, nil
}

// Close closes the database / 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

// encryptSystemState / decryptSystemState encrypt/decrypt the sensitive credentials in
// SystemState in place. Encrypt/Decrypt are idempotent and transparent to legacy plaintext,
// so repeated calls on the read-modify-write path are safe.
// 就地加解密 SystemState 中的敏感凭据；幂等且对历史明文透明。
func (s *Store) encryptSystemState(st *SystemState) error {
	var err error
	if st.RabbitMQAdminPassword, err = s.cipher.Encrypt(st.RabbitMQAdminPassword); err != nil {
		return err
	}
	if st.RedisPassword, err = s.cipher.Encrypt(st.RedisPassword); err != nil {
		return err
	}
	return nil
}

func (s *Store) decryptSystemState(st *SystemState) error {
	var err error
	if st.RabbitMQAdminPassword, err = s.cipher.Decrypt(st.RabbitMQAdminPassword); err != nil {
		return err
	}
	if st.RedisPassword, err = s.cipher.Decrypt(st.RedisPassword); err != nil {
		return err
	}
	return nil
}

// encryptInstance / decryptInstance encrypt/decrypt the broker/redis credentials in
// PluginInstance.RuntimeInfo in place / 就地加解密 RuntimeInfo 中的 broker/redis 凭据.
func (s *Store) encryptInstance(inst *PluginInstance) error {
	if inst.RuntimeInfo == nil {
		return nil
	}
	var err error
	if inst.RuntimeInfo.RabbitMQPassword, err = s.cipher.Encrypt(inst.RuntimeInfo.RabbitMQPassword); err != nil {
		return err
	}
	if inst.RuntimeInfo.RedisPassword, err = s.cipher.Encrypt(inst.RuntimeInfo.RedisPassword); err != nil {
		return err
	}
	return nil
}

func (s *Store) decryptInstance(inst *PluginInstance) error {
	if inst.RuntimeInfo == nil {
		return nil
	}
	var err error
	if inst.RuntimeInfo.RabbitMQPassword, err = s.cipher.Decrypt(inst.RuntimeInfo.RabbitMQPassword); err != nil {
		return err
	}
	if inst.RuntimeInfo.RedisPassword, err = s.cipher.Decrypt(inst.RuntimeInfo.RedisPassword); err != nil {
		return err
	}
	return nil
}

// marshalSystemState serializes an encrypted copy of state without touching the caller's
// plaintext / 序列化 state 的加密副本，不改动调用方的明文.
func (s *Store) marshalSystemState(state *SystemState) ([]byte, error) {
	enc := *state
	if err := s.encryptSystemState(&enc); err != nil {
		return nil, err
	}
	return json.Marshal(&enc)
}

// marshalInstance serializes an encrypted copy of instance (with a deep copy of RuntimeInfo)
// without touching the caller's plaintext / 序列化 instance 的加密副本，不改动调用方的明文.
func (s *Store) marshalInstance(instance *PluginInstance) ([]byte, error) {
	enc := *instance
	if instance.RuntimeInfo != nil {
		ri := *instance.RuntimeInfo
		enc.RuntimeInfo = &ri
	}
	if err := s.encryptInstance(&enc); err != nil {
		return nil, err
	}
	return json.Marshal(&enc)
}

// GetSystemState returns system-level state such as RabbitMQ credentials / 获取系统级状态.
func (s *Store) GetSystemState() (*SystemState, error) {
	var state *SystemState
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSystem)
		data := b.Get([]byte("state"))
		if data == nil {
			return nil
		}
		state = &SystemState{}
		return json.Unmarshal(data, state)
	})
	if err != nil || state == nil {
		return state, err
	}
	return state, s.decryptSystemState(state)
}

// SaveSystemState persists system-level state such as RabbitMQ credentials / 保存系统级状态.
func (s *Store) SaveSystemState(state *SystemState) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSystem)
		data, err := s.marshalSystemState(state)
		if err != nil {
			return err
		}
		return b.Put([]byte("state"), data)
	})
}

// SavePlugin inserts or updates a plugin instance / 新增或更新一个插件实例.
func (s *Store) SavePlugin(instance *PluginInstance) error {
	if instance.InstalledAt.IsZero() {
		instance.InstalledAt = time.Now()
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlugins)
		data, err := s.marshalInstance(instance)
		if err != nil {
			return fmt.Errorf("failed to serialize plugin instance: %w", err)
		}
		return b.Put([]byte(instance.ID), data)
	})
}

// GetPlugin returns a single plugin instance, or nil if absent / 获取单个插件实例，不存在返回 nil.
func (s *Store) GetPlugin(pluginID string) (*PluginInstance, error) {
	var instance *PluginInstance
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlugins)
		data := b.Get([]byte(pluginID))
		if data == nil {
			return nil // absent: return nil without error / 不存在，返回 nil 不报错
		}
		instance = &PluginInstance{}
		return json.Unmarshal(data, instance)
	})
	if err != nil || instance == nil {
		return instance, err
	}
	return instance, s.decryptInstance(instance)
}

// ListPlugins returns all plugin instances / 返回所有插件实例.
func (s *Store) ListPlugins() ([]*PluginInstance, error) {
	var list []*PluginInstance
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlugins)
		return b.ForEach(func(k, v []byte) error {
			instance := &PluginInstance{}
			if err := json.Unmarshal(v, instance); err != nil {
				return err
			}
			if err := s.decryptInstance(instance); err != nil {
				return err
			}
			list = append(list, instance)
			return nil
		})
	})
	return list, err
}

// DeletePlugin removes a plugin instance record / 删除一个插件实例记录.
func (s *Store) DeletePlugin(pluginID string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPlugins).Delete([]byte(pluginID))
	})
}

// UpdateStatus updates only the plugin status, avoiding overwriting other fields / 单独更新插件状态.
func (s *Store) UpdateStatus(pluginID string, status PluginStatus) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlugins)
		data := b.Get([]byte(pluginID))
		if data == nil {
			return fmt.Errorf("plugin %s not found", pluginID)
		}
		instance := &PluginInstance{}
		if err := json.Unmarshal(data, instance); err != nil {
			return err
		}
		instance.Status = status
		updated, err := s.marshalInstance(instance)
		if err != nil {
			return err
		}
		return b.Put([]byte(pluginID), updated)
	})
}

// UpdateRuntimeInfo updates runtime info after startup / 启动后更新运行时信息.
func (s *Store) UpdateRuntimeInfo(pluginID string, info *RuntimeInfo) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPlugins)
		data := b.Get([]byte(pluginID))
		if data == nil {
			return fmt.Errorf("plugin %s not found", pluginID)
		}
		instance := &PluginInstance{}
		if err := json.Unmarshal(data, instance); err != nil {
			return err
		}
		instance.RuntimeInfo = info
		updated, err := s.marshalInstance(instance)
		if err != nil {
			return err
		}
		return b.Put([]byte(pluginID), updated)
	})
}
