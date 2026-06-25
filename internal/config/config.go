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
	// DB file path, default ~/.griffino/griffino.db / 数据库路径
	DatabasePath string    `yaml:"databasePath"`
	Server       Server    `yaml:"server"`
	RabbitMQ     RabbitMQ  `yaml:"rabbitmq"`
	Docker       Docker    `yaml:"docker"`
	Telemetry    Telemetry `yaml:"telemetry"`
}

// Telemetry holds observability config / 可观测性配置.
// An empty OTLPEndpoint disables tracing (the default, no collector needed); set
// an OTLP/HTTP endpoint to enable trace export.
// OTLPEndpoint 为空时关闭追踪（默认，无需 collector）；填入 OTLP/HTTP 端点后启用导出。
type Telemetry struct {
	OTLPEndpoint string `yaml:"otlpEndpoint"`
}

// Server controls the HTTP API listen address / HTTP API 监听地址.
// Binds to 127.0.0.1 only by default; no LAN/multi-node access for the first few
// major versions. Override the server section in config.yaml to change the port or
// (at your own risk) expose it externally.
// 默认仅绑定本机（127.0.0.1），前几个大版本不开放局域网/多机访问；改端口或（自负风险）对外暴露请覆盖 config.yaml 的 server 段。
type Server struct {
	ListenHost string `yaml:"listenHost"`
	ListenPort int    `yaml:"listenPort"`
}

type RabbitMQ struct {
	Host           string `yaml:"host"`
	ContainerHost  string `yaml:"containerHost"` // host as seen from inside containers / 容器侧 host
	Port           int    `yaml:"port"`
	ManagementPort int    `yaml:"managementPort"`
	AdminUser      string `yaml:"adminUser"`
	AdminPassword  string `yaml:"adminPassword"`
}

type Docker struct {
	// empty uses the default socket (/var/run/docker.sock) / 留空用默认 socket
	Host string `yaml:"host"`
}

// DefaultConfigPath returns the default config path ~/.griffino/config.yaml / 默认配置路径.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".griffino", "config.yaml")
}

// DefaultDatabasePath returns the default database path ~/.griffino/griffino.db / 默认数据库路径.
func DefaultDatabasePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".griffino", "griffino.db")
}

// SocketPath returns the daemon Unix socket path ~/.griffino/daemon.sock / daemon Unix socket 路径.
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".griffino", "daemon.sock")
}

// Load loads config from file; returns defaults if the file is absent / 从文件加载配置，文件不存在则返回默认配置.
func Load(path string) (*Config, error) {
	cfg := &Config{
		DatabasePath: DefaultDatabasePath(),
		Server: Server{
			ListenHost: "127.0.0.1",
			ListenPort: 7070,
		},
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
		return cfg, nil // file absent: use defaults / 文件不存在用默认值
	}
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	return cfg, nil
}
