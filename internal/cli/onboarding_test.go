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

package cli

import (
	"os"
	"path/filepath"
	"testing"

	griffinoi18n "github.com/GriffinGuard/Griffino/internal/i18n"
	"github.com/GriffinGuard/Griffino/internal/store"
)

func TestMain(m *testing.M) {
	_ = griffinoi18n.Init("en") // so i18n.T() in cli code doesn't nil-panic
	os.Exit(m.Run())
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestBootstrapAdminCLICreatesAdmin(t *testing.T) {
	s := newTestStore(t)
	if err := bootstrapAdmin(s, "cli"); err != nil {
		t.Fatalf("bootstrapAdmin(cli): %v", err)
	}
	if has, _ := s.HasAnyUser(); !has {
		t.Fatal("cli mode should have created an admin")
	}
}

func TestBootstrapAdminWebDefersCreation(t *testing.T) {
	s := newTestStore(t)
	if err := bootstrapAdmin(s, "web"); err != nil {
		t.Fatalf("bootstrapAdmin(web): %v", err)
	}
	if has, _ := s.HasAnyUser(); has {
		t.Fatal("web mode should NOT create an admin (deferred to the wizard)")
	}
}

func TestBootstrapAdminUnattendedFromEnv(t *testing.T) {
	t.Setenv("GRIFFINO_ADMIN_USER", "root")
	t.Setenv("GRIFFINO_ADMIN_PASSWORD", "supersecret")
	s := newTestStore(t)
	if err := bootstrapAdmin(s, "unattended"); err != nil {
		t.Fatalf("bootstrapAdmin(unattended): %v", err)
	}
	u, err := s.GetUserByUsername("root")
	if err != nil || u == nil {
		t.Fatalf("expected env-provisioned admin 'root': user=%v err=%v", u, err)
	}
	if u.MustChange {
		t.Error("an env-provisioned admin should not be forced to change password")
	}
}

func TestBootstrapAdminIsNoOpWhenUserExists(t *testing.T) {
	s := newTestStore(t)
	if err := bootstrapAdmin(s, "cli"); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	// A second call must not error or create a second user.
	if err := bootstrapAdmin(s, "cli"); err != nil {
		t.Fatalf("second bootstrap should be a no-op: %v", err)
	}
	users, err := s.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected exactly 1 user, got %d", len(users))
	}
}
