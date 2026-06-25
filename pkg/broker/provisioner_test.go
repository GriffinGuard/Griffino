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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// fakeClient implements BrokerClient, recording call sequences and allowing per-method error injection / 实现 BrokerClient，记录调用序列并允许注入错误
type fakeClient struct {
	calls   []string
	failOn  string // Returns error when a method name matches / 当某个方法名匹配时返回错误
	failErr error
}

func (f *fakeClient) record(name string) error {
	f.calls = append(f.calls, name)
	if f.failOn != "" && strings.HasPrefix(name, f.failOn) {
		if f.failErr != nil {
			return f.failErr
		}
		return errors.New("injected failure")
	}
	return nil
}

func (f *fakeClient) HealthCheck() error { return f.record("HealthCheck") }
func (f *fakeClient) CreateUser(username, password string) error {
	return f.record("CreateUser:" + username)
}
func (f *fakeClient) DeleteUser(username string) error { return f.record("DeleteUser:" + username) }
func (f *fakeClient) SetPermissions(username, vhost, configure, write, read string) error {
	return f.record("SetPermissions:" + username)
}
func (f *fakeClient) DeclareExchange(vhost, name, kind string, durable bool) error {
	return f.record("DeclareExchange:" + name)
}
func (f *fakeClient) DeclareQueue(vhost, name string, durable bool) error {
	return f.record("DeclareQueue:" + name)
}
func (f *fakeClient) BindQueue(vhost, queue, exchange, routingKey string) error {
	return f.record(fmt.Sprintf("BindQueue:%s->%s[%s]", queue, exchange, routingKey))
}
func (f *fakeClient) SetUserPassword(username, password string) error {
	return f.record("SetUserPassword:" + username)
}

func testManifest() *manifest.PluginManifest {
	return &manifest.PluginManifest{
		ID: "acme.thing",
		Capabilities: []manifest.Capability{
			{
				ID:   "cap1",
				Role: "provider",
				EntryPoint: manifest.EntryPoint{
					Type: "rabbitmq_topic",
					Details: manifest.EntryPointDetails{
						RequestTopicPattern: "req.cap1",
					},
				},
			},
		},
	}
}

func TestProvisionCallSequence(t *testing.T) {
	fake := &fakeClient{}
	p := NewProvisioner(fake, "/")

	res, err := p.Provision(testManifest())
	if err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	wantUser := "griffino.plugin.acme.thing"
	if res.Username != wantUser {
		t.Errorf("Username = %q, want %q", res.Username, wantUser)
	}
	if res.Password == "" {
		t.Error("Password should not be empty")
	}
	if res.Vhost != "/" {
		t.Errorf("Vhost = %q, want /", res.Vhost)
	}

	// Expected call sequence: create user → set permissions → declare cap queue + bind → declare action queue + bind / 期望调用序列
	want := []string{
		"CreateUser:" + wantUser,
		"SetPermissions:" + wantUser,
		"DeclareQueue:plugin.acme.thing.cap1",
		"BindQueue:plugin.acme.thing.cap1->griffino.plugins[req.cap1]",
		"DeclareQueue:action.acme.thing",
		"BindQueue:action.acme.thing->griffino.actions[action.acme.thing.#]",
	}
	if got := strings.Join(fake.calls, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("call sequence mismatch:\n got:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}

	// Queue should include both cap queue and action queue / 队列应包含 cap 队列与 action 队列
	wantQueues := []string{"plugin.acme.thing.cap1", "action.acme.thing"}
	if len(res.Queues) != len(wantQueues) {
		t.Fatalf("Queues = %v, want %v", res.Queues, wantQueues)
	}
	for i, q := range wantQueues {
		if res.Queues[i] != q {
			t.Errorf("Queues[%d] = %q, want %q", i, res.Queues[i], q)
		}
	}
}

func TestProvisionCreateUserFails(t *testing.T) {
	fake := &fakeClient{failOn: "CreateUser"}
	p := NewProvisioner(fake, "/")

	_, err := p.Provision(testManifest())
	if err == nil {
		t.Fatal("expected error when CreateUser fails")
	}
	if !errors.Is(err, ErrCreateUser) {
		t.Errorf("error = %v, want wraps ErrCreateUser", err)
	}
	// After user creation fails, no further calls should be made / 用户创建失败后不应继续后续调用
	if len(fake.calls) != 1 {
		t.Errorf("expected to stop after CreateUser, calls = %v", fake.calls)
	}
}

func TestProvisionBindQueueFails(t *testing.T) {
	fake := &fakeClient{failOn: "BindQueue"}
	p := NewProvisioner(fake, "/")

	_, err := p.Provision(testManifest())
	if err == nil {
		t.Fatal("expected error when BindQueue fails")
	}
	if !errors.Is(err, ErrBindQueue) {
		t.Errorf("error = %v, want wraps ErrBindQueue", err)
	}
}

func TestTeardownCallsDeleteUser(t *testing.T) {
	fake := &fakeClient{}
	p := NewProvisioner(fake, "/")

	res := &PluginResources{Username: "griffino.plugin.acme.thing"}
	if err := p.Teardown(res); err != nil {
		t.Fatalf("Teardown() error = %v", err)
	}
	want := []string{"DeleteUser:griffino.plugin.acme.thing"}
	if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", fake.calls, want)
	}
}

func TestTeardownDeleteUserFails(t *testing.T) {
	fake := &fakeClient{failOn: "DeleteUser"}
	p := NewProvisioner(fake, "/")

	err := p.Teardown(&PluginResources{Username: "u"})
	if err == nil {
		t.Fatal("expected error when DeleteUser fails")
	}
	if !errors.Is(err, ErrDeleteUser) {
		t.Errorf("error = %v, want wraps ErrDeleteUser", err)
	}
}

func TestSyncCredentialsCallSequence(t *testing.T) {
	fake := &fakeClient{}
	p := NewProvisioner(fake, "/")

	if err := p.SyncCredentials("griffino.plugin.acme.thing", "newpw", testManifest()); err != nil {
		t.Fatalf("SyncCredentials() error = %v", err)
	}
	want := []string{
		"SetUserPassword:griffino.plugin.acme.thing",
		"SetPermissions:griffino.plugin.acme.thing",
	}
	if strings.Join(fake.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", fake.calls, want)
	}
}

func TestSyncCredentialsSetPasswordFails(t *testing.T) {
	fake := &fakeClient{failOn: "SetUserPassword"}
	p := NewProvisioner(fake, "/")

	err := p.SyncCredentials("u", "pw", testManifest())
	if err == nil {
		t.Fatal("expected error when SetUserPassword fails")
	}
	if !errors.Is(err, ErrSetUserPassword) {
		t.Errorf("error = %v, want wraps ErrSetUserPassword", err)
	}
}
