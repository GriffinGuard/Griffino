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

package auth

import (
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestRefreshExtendsUserSessionIndexTTL(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	host, portStr, err := net.SplitHostPort(mr.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}
	mgr := NewSessionManager(host, port, "")

	token, err := mgr.Create(t.Context(), SessionData{
		UserID:   "u1",
		Username: "alice",
		Role:     "user",
	}, time.Second)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	mr.FastForward(500 * time.Millisecond)
	if err := mgr.Refresh(t.Context(), token, 2*time.Second); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	mr.FastForward(time.Second)
	sessions, err := mgr.ListByUser(t.Context(), "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1", len(sessions))
	}
}
