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
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturedRequest records one request the server received, for assertions / 记录 server 收到的一次请求供断言.
type capturedRequest struct {
	method string
	path   string
	user   string
	pass   string
	hasBA  bool
}

// newTestServer returns a server that records requests into *capturedRequest and responds
// with status, plus a Client pointing at it / 返回记录请求并按 status 响应的 server 及其 Client.
func newTestServer(t *testing.T, status int) (*Client, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.user, captured.pass, captured.hasBA = r.BasicAuth()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	c := newClientWithBaseURL(srv.URL+"/api", "admin", "secret")
	return c, captured
}

func TestClientHealthCheck(t *testing.T) {
	c, captured := newTestServer(t, http.StatusOK)
	if err := c.HealthCheck(); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
	if captured.method != http.MethodGet {
		t.Errorf("method = %q, want GET", captured.method)
	}
	if captured.path != "/api/healthchecks/node" {
		t.Errorf("path = %q, want /api/healthchecks/node", captured.path)
	}
	if !captured.hasBA || captured.user != "admin" || captured.pass != "secret" {
		t.Errorf("basic auth = (%q,%q,%v), want (admin,secret,true)", captured.user, captured.pass, captured.hasBA)
	}
}

func TestClientHealthCheckError(t *testing.T) {
	c, _ := newTestServer(t, http.StatusServiceUnavailable)
	if err := c.HealthCheck(); err == nil {
		t.Fatal("HealthCheck() expected error on 503, got nil")
	}
}

func TestClientMethodsRequestShape(t *testing.T) {
	tests := []struct {
		name       string
		call       func(c *Client) error
		wantMethod string
		wantPath   string
	}{
		{
			name:       "CreateUser",
			call:       func(c *Client) error { return c.CreateUser("alice", "pw") },
			wantMethod: http.MethodPut,
			wantPath:   "/api/users/alice",
		},
		{
			name:       "DeleteUser",
			call:       func(c *Client) error { return c.DeleteUser("alice") },
			wantMethod: http.MethodDelete,
			wantPath:   "/api/users/alice",
		},
		{
			name:       "SetUserPassword",
			call:       func(c *Client) error { return c.SetUserPassword("alice", "pw") },
			wantMethod: http.MethodPut,
			wantPath:   "/api/users/alice",
		},
		{
			name:       "SetPermissions",
			call:       func(c *Client) error { return c.SetPermissions("alice", "/", "c", "w", "r") },
			wantMethod: http.MethodPut,
			wantPath:   "/api/permissions/%2F/alice",
		},
		{
			name:       "DeclareExchange",
			call:       func(c *Client) error { return c.DeclareExchange("/", "ex", "topic", true) },
			wantMethod: http.MethodPut,
			wantPath:   "/api/exchanges/%2F/ex",
		},
		{
			name:       "DeclareQueue",
			call:       func(c *Client) error { return c.DeclareQueue("/", "q", true) },
			wantMethod: http.MethodPut,
			wantPath:   "/api/queues/%2F/q",
		},
		{
			name:       "BindQueue",
			call:       func(c *Client) error { return c.BindQueue("/", "q", "ex", "rk") },
			wantMethod: http.MethodPost,
			wantPath:   "/api/bindings/%2F/e/ex/q/q",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captured := &capturedRequest{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured.method = r.Method
				// EscapedPath keeps %2F encoding, making vhost escaping easy to assert / 保留 %2F 编码便于断言 vhost 转义
				captured.path = r.URL.EscapedPath()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer srv.Close()

			c := newClientWithBaseURL(srv.URL+"/api", "admin", "secret")
			if err := tt.call(c); err != nil {
				t.Fatalf("%s returned error: %v", tt.name, err)
			}
			if captured.method != tt.wantMethod {
				t.Errorf("method = %q, want %q", captured.method, tt.wantMethod)
			}
			if captured.path != tt.wantPath {
				t.Errorf("path = %q, want %q", captured.path, tt.wantPath)
			}
		})
	}
}

func TestClientMethodsErrorStatus(t *testing.T) {
	// Every method should return an error on a non-success status code (500) / 非成功状态码（500）时每个方法都应报错.
	calls := map[string]func(c *Client) error{
		"CreateUser":      func(c *Client) error { return c.CreateUser("a", "p") },
		"DeleteUser":      func(c *Client) error { return c.DeleteUser("a") },
		"SetUserPassword": func(c *Client) error { return c.SetUserPassword("a", "p") },
		"SetPermissions":  func(c *Client) error { return c.SetPermissions("a", "/", "c", "w", "r") },
		"DeclareExchange": func(c *Client) error { return c.DeclareExchange("/", "ex", "topic", true) },
		"DeclareQueue":    func(c *Client) error { return c.DeclareQueue("/", "q", true) },
		"BindQueue":       func(c *Client) error { return c.BindQueue("/", "q", "ex", "rk") },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			c, _ := newTestServer(t, http.StatusInternalServerError)
			if err := call(c); err == nil {
				t.Errorf("%s expected error on 500, got nil", name)
			}
		})
	}
}

func TestDeleteUserNotFoundIsOK(t *testing.T) {
	// DeleteUser treats 404 as success (idempotent delete) / DeleteUser 把 404 视为成功（幂等删除）.
	c, _ := newTestServer(t, http.StatusNotFound)
	if err := c.DeleteUser("ghost"); err != nil {
		t.Errorf("DeleteUser on 404 should be nil, got %v", err)
	}
}
