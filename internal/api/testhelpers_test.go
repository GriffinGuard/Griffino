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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/GriffinGuard/Griffino/internal/auth"
	"github.com/GriffinGuard/Griffino/internal/router"
	"github.com/GriffinGuard/Griffino/internal/store"
	"github.com/GriffinGuard/Griffino/internal/taskscheduler"
	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// fakePluginService is a configurable test double satisfying the api.PluginService
// interface. Each method returns the corresponding error field, letting tests drive
// handler success/error branches without Docker or RabbitMQ.
type fakePluginService struct {
	startAsyncErr error
	startInstErr  error
	stopErr       error
	stopThenStart error
	uninstallErr  error
	upgradeErr    error
}

func (f *fakePluginService) StartPluginAsync(pluginID string) error { return f.startAsyncErr }
func (f *fakePluginService) StartPluginFromInstance(instance *store.PluginInstance) error {
	return f.startInstErr
}
func (f *fakePluginService) StopPlugin(ctx context.Context, pluginID string) error { return f.stopErr }
func (f *fakePluginService) StopThenStartAsync(pluginID string) error              { return f.stopThenStart }
func (f *fakePluginService) UninstallPlugin(ctx context.Context, pluginID string) error {
	return f.uninstallErr
}
func (f *fakePluginService) UpgradePlugin(ctx context.Context, pluginID, newDir string, newPkg *manifest.PluginPackage) error {
	return f.upgradeErr
}

var _ PluginService = (*fakePluginService)(nil)

// newTestStore builds a fresh BoltDB-backed store in a temp dir.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// newTestRedis starts an in-memory redis and returns its host:port address.
func newTestRedis(t *testing.T) string {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr.Addr()
}

// splitHostPort splits "host:port" into its parts for NewSessionManager.
func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("bad addr %q", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("bad port in %q: %v", addr, err)
	}
	return host, port
}

// newTestServer constructs a *Server directly as a struct literal (NOT via NewServer,
// which requires a system.Manager + Docker client). Only the fields the handlers under
// test reference are populated. Pass options to wire up redis-backed stores and a fake
// plugin service.
type serverOpts struct {
	withSession bool // wire sessionMgr (auth/login tests)
	withRoutes  bool // wire routeStore (routes tests)
	withBP      bool // wire bpStore + schemaStore (blueprint tests)
	pluginSvc   PluginService
}

func newTestServer(t *testing.T, opts serverOpts) *Server {
	t.Helper()
	st := newTestStore(t)
	s := &Server{
		st:        st,
		pluginSvc: opts.pluginSvc,
	}
	if opts.withSession || opts.withRoutes {
		addr := newTestRedis(t)
		if opts.withSession {
			host, port := splitHostPort(t, addr)
			s.sessionMgr = auth.NewSessionManager(host, port, "")
		}
		if opts.withRoutes {
			s.routeStore = router.NewRouteStore(addr, "")
		}
	}
	if opts.withBP {
		s.bpStore = taskscheduler.NewBlueprintStore(st.DB())
		s.schemaStore = taskscheduler.NewSchemaStore(st.DB())
	}
	return s
}

// withSession attaches an authenticated SessionData to the request context, mimicking
// what authMiddleware does, so handlers that read sessionKey work in isolation.
func withSession(r *http.Request, sd *auth.SessionData) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), sessionKey, sd))
}

// decodeError extracts the AppError from a JSON error response body.
func decodeError(t *testing.T, body []byte) AppError {
	t.Helper()
	var wrap struct {
		Error AppError `json:"error"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		t.Fatalf("decode error body %q: %v", string(body), err)
	}
	return wrap.Error
}

// assertStatus fails the test if the recorder's status differs from want.
func assertStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", rr.Code, want, rr.Body.String())
	}
}

// assertErrorCode asserts both the HTTP status and the JSON error.code.
func assertErrorCode(t *testing.T, rr *httptest.ResponseRecorder, wantStatus int, wantCode ErrorCode) {
	t.Helper()
	assertStatus(t, rr, wantStatus)
	if got := decodeError(t, rr.Body.Bytes()).Code; got != wantCode {
		t.Fatalf("error code = %q, want %q", got, wantCode)
	}
}
