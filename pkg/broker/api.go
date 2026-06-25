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

import "net/http"

// BrokerClient abstracts the RabbitMQ Management API operations needed by the Provisioner and callers / 抽象了 Provisioner 及调用方所需的 RabbitMQ Management API 操作
// *Client implements this interface; tests can substitute a fake to avoid real HTTP calls / *Client 实现了该接口，测试中可用 fake 替换避免真实 HTTP 调用
type BrokerClient interface {
	HealthCheck() error
	CreateUser(username, password string) error
	DeleteUser(username string) error
	SetPermissions(username, vhost, configure, write, read string) error
	DeclareExchange(vhost, name, kind string, durable bool) error
	DeclareQueue(vhost, name string, durable bool) error
	BindQueue(vhost, queue, exchange, routingKey string) error
	SetUserPassword(username, password string) error
}

// Compile-time assertion: *Client must satisfy BrokerClient / 编译期断言：*Client 必须满足 BrokerClient 接口
var _ BrokerClient = (*Client)(nil)

// newClientWithBaseURL builds a Client from a full baseURL, mainly so tests can point
// requests at an httptest.Server. The public API (NewClient) is unchanged.
// 用完整 baseURL 构造 Client，主要供测试指向 httptest.Server。
func newClientWithBaseURL(baseURL, adminUser, adminPass string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
		adminUser:  adminUser,
		adminPass:  adminPass,
	}
}
