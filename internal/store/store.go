package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketSystem = []byte("system")
var bucketPlugins = []byte("plugins")
var bucketBlueprints = []byte("blueprints")
var bucketSchemas = []byte("schemas")

// SystemState 存储系统级状态，如 RabbitMQ 凭据等
type SystemState struct {
    RabbitMQAdminUser      string `json:"rabbitmqAdminUser"`
    RabbitMQAdminPassword  string `json:"rabbitmqAdminPassword"`
    RabbitMQPort           int    `json:"rabbitmqPort"`
    RabbitMQManagementPort int    `json:"rabbitmqManagementPort"`
    RedisPort              int    `json:"redisPort"`
    RedisPassword          string `json:"redisPassword"`          // 系统 Redis 认证密码
    RabbitMQContainerName  string `json:"rabbitmqContainerName"` // 自定义时不为空
    RedisContainerName     string `json:"redisContainerName"`    // 自定义时不为空
}

// PluginStatus 插件实例的当前状态
type PluginStatus string

const (
    StatusPendingSetup PluginStatus = "pending_setup" // 元文件已下载，未完成配置
    StatusReady        PluginStatus = "ready"          // 配置完成，从未启动
    StatusPulling      PluginStatus = "pulling"        // 正在拉取镜像
    StatusStarting     PluginStatus = "starting"       // 镜像就绪，容器启动中
    StatusRunning      PluginStatus = "running"        // 运行中
    StatusStopped      PluginStatus = "stopped"        // 已停止
    StatusFailed       PluginStatus = "failed"         // 启动失败
)

// PluginInstance 单个插件实例的完整状态
type PluginInstance struct {
    ID          string                       `json:"id"`
    PluginDir   string                       `json:"pluginDir"`
    Status      PluginStatus                 `json:"status"`
    InstalledAt time.Time                    `json:"installedAt"`
    IsDevPlugin  bool                         `json:"isDevPlugin,omitempty"`
    AdminConfig  map[string]map[string]string `json:"adminConfig,omitempty"`
    RuntimeInfo  *RuntimeInfo                 `json:"runtimeInfo,omitempty"`
    ConfigDirty  bool                         `json:"configDirty,omitempty"`
    FailReason   string                       `json:"failReason,omitempty"`
    FailStage    string                       `json:"failStage,omitempty"` // "pull" | "start"
}

// RuntimeInfo 插件启动后的运行时信息
type RuntimeInfo struct {
    Containers   map[string]string `json:"containers"`   // map[serviceId]containerName
    Network      string            `json:"network"`
    RabbitMQUser string            `json:"rabbitmqUser"`
    RabbitMQPassword string           `json:"rabbitmqPassword"`
    RedisUser        string            `json:"redisUser"`
	RedisPassword    string            `json:"redisPassword"`
}

// Store 负责状态的读写
type Store struct {
    db *bolt.DB
}

// DB 暴露底层 bolt.DB，供需要直接操作 BoltDB 的子系统使用
func (s *Store) DB() *bolt.DB {
    return s.db
}

// New 初始化 Store，dbPath 是数据库文件路径（如 ~/.griffino/griffino.db）
func New(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
        return nil, fmt.Errorf("failed to create database directory: %w", err)
    }

    db, err := bolt.Open(dbPath, 0644, &bolt.Options{Timeout: 1 * time.Second})
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }

    // 确保 bucket 存在
    err = db.Update(func(tx *bolt.Tx) error {
        if _, err := tx.CreateBucketIfNotExists(bucketPlugins); err != nil {
            return err
        }
        if _, err := tx.CreateBucketIfNotExists(bucketSystem); err != nil {
            return err
        }
        if _, err := tx.CreateBucketIfNotExists(bucketBlueprints); err != nil {
            return err
        }
        _, err := tx.CreateBucketIfNotExists(bucketSchemas)
        return err
    })

    return &Store{db: db}, nil
}

// Close 关闭数据库
func (s *Store) Close() error {
    return s.db.Close()
}

// GetSystemState 获取系统级状态，如 RabbitMQ 凭据等
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
	return state, err
}

// SaveSystemState 保存系统级状态，如 RabbitMQ 凭据等
func (s *Store) SaveSystemState(state *SystemState) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSystem)
		data, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return b.Put([]byte("state"), data)
	})
}

// SavePlugin 新增或更新一个插件实例
func (s *Store) SavePlugin(instance *PluginInstance) error {
    if instance.InstalledAt.IsZero() {
        instance.InstalledAt = time.Now()
    }
    return s.db.Update(func(tx *bolt.Tx) error {
        b := tx.Bucket(bucketPlugins)
        data, err := json.Marshal(instance)
        if err != nil {
            return fmt.Errorf("failed to serialize plugin instance: %w", err)
        }
        return b.Put([]byte(instance.ID), data)
    })
}

// GetPlugin 获取单个插件实例，不存在返回 nil
func (s *Store) GetPlugin(pluginID string) (*PluginInstance, error) {
    var instance *PluginInstance
    err := s.db.View(func(tx *bolt.Tx) error {
        b := tx.Bucket(bucketPlugins)
        data := b.Get([]byte(pluginID))
        if data == nil {
            return nil // 不存在，返回 nil 不报错
        }
        instance = &PluginInstance{}
        return json.Unmarshal(data, instance)
    })
    return instance, err
}

// ListPlugins 返回所有插件实例
func (s *Store) ListPlugins() ([]*PluginInstance, error) {
    var list []*PluginInstance
    err := s.db.View(func(tx *bolt.Tx) error {
        b := tx.Bucket(bucketPlugins)
        return b.ForEach(func(k, v []byte) error {
            instance := &PluginInstance{}
            if err := json.Unmarshal(v, instance); err != nil {
                return err
            }
            list = append(list, instance)
            return nil
        })
    })
    return list, err
}

// DeletePlugin 删除一个插件实例记录
func (s *Store) DeletePlugin(pluginID string) error {
    return s.db.Update(func(tx *bolt.Tx) error {
        return tx.Bucket(bucketPlugins).Delete([]byte(pluginID))
    })
}

// UpdateStatus 单独更新插件状态，避免覆盖其他字段
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
        updated, err := json.Marshal(instance)
        if err != nil {
            return err
        }
        return b.Put([]byte(pluginID), updated)
    })
}

// UpdateRuntimeInfo 启动后更新运行时信息
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
        updated, err := json.Marshal(instance)
        if err != nil {
            return err
        }
        return b.Put([]byte(pluginID), updated)
    })
}