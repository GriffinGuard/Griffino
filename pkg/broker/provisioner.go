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

package broker

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

type ProvisionLogger interface {
	Log(text string)
}

type noopLogger struct{}

func (noopLogger) Log(string) {}

// ProvisionLoggerFunc 函数类型适配器，方便调用方注入匿名函数
type ProvisionLoggerFunc func(string)

func (f ProvisionLoggerFunc) Log(text string) { f(text) }

type Provisioner struct {
	client *Client
	vhost  string
	logger ProvisionLogger
}

func NewProvisioner(client *Client, vhost string) *Provisioner {
	return &Provisioner{client: client, vhost: vhost, logger: noopLogger{}}
}

func (p *Provisioner) SetLogger(l ProvisionLogger) {
	p.logger = l
}

// PluginResources 为插件创建的 RabbitMQ 资源信息
type PluginResources struct {
	Username  string
	Password  string
	Vhost     string
	Exchanges []string
	Queues    []string
}

// Provision 根据插件 manifest 中的 capabilities 创建所需的 RabbitMQ 资源
func (p *Provisioner) Provision(m *manifest.PluginManifest) (*PluginResources, error) {
	username := fmt.Sprintf("griffino.plugin.%s", m.ID)
	password, err := generatePassword()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGeneratePassword, err)
	}

	p.logger.Log(fmt.Sprintf("creating RabbitMQ user: %s", username))
	if err := p.client.CreateUser(username, password); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCreateUser, err)
	}

	if err := p.setPermissions(username, m.ID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSetPermissions, err)
	}

	resources := &PluginResources{
		Username:  username,
		Password:  password,
		Vhost:     p.vhost,
		Exchanges: []string{},
		Queues:    []string{},
	}

	for _, cap := range m.Capabilities {
		if cap.EntryPoint.Type != "rabbitmq_topic" {
			continue
		}
		if cap.Role != "provider" {
			continue
		}
		requestTopic := cap.EntryPoint.Details.RequestTopicPattern
		if requestTopic == "" {
			continue
		}

		queueName := fmt.Sprintf("plugin.%s.%s", m.ID, cap.ID)
		p.logger.Log(fmt.Sprintf("declaring queue: %s", queueName))
		if err := p.client.DeclareQueue(p.vhost, queueName, true); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDeclareQueue, err)
		}

		p.logger.Log(fmt.Sprintf("binding queue %s → griffino.plugins [%s]", queueName, requestTopic))
		if err := p.client.BindQueue(p.vhost, queueName, "griffino.plugins", requestTopic); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBindQueue, err)
		}

		resources.Queues = append(resources.Queues, queueName)
	}

	// 声明 actions 队列（接收所有发给此插件的动作消息）
	// 队列命名：action.{pluginId}，binding key：action.{pluginId}.#
	actionsQueue := fmt.Sprintf("action.%s", m.ID)
	p.logger.Log(fmt.Sprintf("declaring actions queue: %s", actionsQueue))
	if err := p.client.DeclareQueue(p.vhost, actionsQueue, true); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDeclareQueue, err)
	}

	p.logger.Log(fmt.Sprintf("binding actions queue %s → griffino.actions [action.%s.#]", actionsQueue, m.ID))
	bindingKey := fmt.Sprintf("action.%s.#", m.ID)
	if err := p.client.BindQueue(p.vhost, actionsQueue, "griffino.actions", bindingKey); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBindQueue, err)
	}

	resources.Queues = append(resources.Queues, actionsQueue)

	return resources, nil
}

// Teardown 清理插件的所有 RabbitMQ 资源
func (p *Provisioner) Teardown(resources *PluginResources) error {
	p.logger.Log(fmt.Sprintf("deleting RabbitMQ user: %s", resources.Username))
	if err := p.client.DeleteUser(resources.Username); err != nil {
		return fmt.Errorf("%w: %v", ErrDeleteUser, err)
	}
	return nil
}

// SyncCredentials 将已有凭据重新同步到 RabbitMQ（用于重启场景）
func (p *Provisioner) SyncCredentials(username, password string, m *manifest.PluginManifest) error {
	p.logger.Log(fmt.Sprintf("syncing RabbitMQ credentials: %s", username))
	if err := p.client.SetUserPassword(username, password); err != nil {
		return fmt.Errorf("%w: %v", ErrSetUserPassword, err)
	}
	return p.setPermissions(username, m.ID)
}

func generatePassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (p *Provisioner) setPermissions(username, pluginID string) error {
	pluginIDEscaped := strings.ReplaceAll(pluginID, ".", "\\.")
	configurePattern := fmt.Sprintf("^(plugin\\.%s\\..*|action\\.%s|amq\\.gen-.*)", pluginIDEscaped, pluginIDEscaped)
	writePattern     := fmt.Sprintf("^(griffino\\.plugins|griffino\\.router|plugin\\.%s\\..*|amq\\.gen-.*|amq\\.default)", pluginIDEscaped)
	readPattern      := fmt.Sprintf("^(griffino\\.plugins|griffino\\.router|griffino\\.actions|plugin\\.%s\\..*|action\\.%s|amq\\.gen-.*)", pluginIDEscaped, pluginIDEscaped)
	return p.client.SetPermissions(username, p.vhost, configurePattern, writePattern, readPattern)
}