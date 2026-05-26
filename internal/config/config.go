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

package config

import (
    "fmt"
    "os"
    "path/filepath"

    "gopkg.in/yaml.v3"
)

type Config struct {
    // 数据库文件路径，默认 ~/.griffino/griffino.db
    DatabasePath string     `yaml:"databasePath"`
    RabbitMQ     RabbitMQ   `yaml:"rabbitmq"`
    Docker       Docker     `yaml:"docker"`
}

type RabbitMQ struct {
    Host           string `yaml:"host"`
    ContainerHost  string `yaml:"containerHost"` // 新增
    Port           int    `yaml:"port"`
    ManagementPort int    `yaml:"managementPort"`
    AdminUser      string `yaml:"adminUser"`
    AdminPassword  string `yaml:"adminPassword"`
}

type Docker struct {
    // 留空则使用默认 socket（/var/run/docker.sock）
    Host string `yaml:"host"`
}

// DefaultConfigPath 返回默认配置文件路径 ~/.griffino/config.yaml
func DefaultConfigPath() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".griffino", "config.yaml")
}

// DefaultDatabasePath 返回默认数据库路径 ~/.griffino/griffino.db
func DefaultDatabasePath() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".griffino", "griffino.db")
}

// SocketPath 返回 daemon Unix Socket 路径 ~/.griffino/daemon.sock
func SocketPath() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".griffino", "daemon.sock")
}

// Load 从文件加载配置，文件不存在则返回默认配置
func Load(path string) (*Config, error) {
    cfg := &Config{
        DatabasePath: DefaultDatabasePath(),
        RabbitMQ: RabbitMQ{
            Host:           "localhost",
            Port:           5672,
            ManagementPort: 15672,
            AdminUser:      "guest",
            AdminPassword:  "guest",
        },
    }

    data, err := os.ReadFile(path)
    if os.IsNotExist(err) {
        return cfg, nil // 文件不存在时返回默认配置
    }
    if err != nil {
        return nil, fmt.Errorf("读取配置文件失败: %w", err)
    }

    if err := yaml.Unmarshal(data, cfg); err != nil {
        return nil, fmt.Errorf("解析配置文件失败: %w", err)
    }
    return cfg, nil
}