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

package store

import (
	"path/filepath"
	"testing"
)

func TestSecurityPoliciesDefaults(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	p, err := s.GetSecurityPolicies()
	if err != nil {
		t.Fatalf("GetSecurityPolicies on fresh db: %v", err)
	}
	def := defaultSecurityPolicies()
	if p != def {
		t.Fatalf("fresh db: got %+v, want %+v", p, def)
	}
}

func TestSecurityPoliciesRoundTrip(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	want := SecurityPolicies{
		SessionTTLHours:        48,
		MaxLoginAttempts:       3,
		LockoutDurationMinutes: 30,
	}
	if err := s.SetSecurityPolicies(want); err != nil {
		t.Fatalf("SetSecurityPolicies: %v", err)
	}
	got, err := s.GetSecurityPolicies()
	if err != nil {
		t.Fatalf("GetSecurityPolicies: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}

func TestSecurityPoliciesOverwrite(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	first := SecurityPolicies{SessionTTLHours: 24, MaxLoginAttempts: 10, LockoutDurationMinutes: 5}
	second := SecurityPolicies{SessionTTLHours: 720, MaxLoginAttempts: 2, LockoutDurationMinutes: 60}

	if err := s.SetSecurityPolicies(first); err != nil {
		t.Fatalf("SetSecurityPolicies first: %v", err)
	}
	if err := s.SetSecurityPolicies(second); err != nil {
		t.Fatalf("SetSecurityPolicies second: %v", err)
	}
	got, err := s.GetSecurityPolicies()
	if err != nil {
		t.Fatalf("GetSecurityPolicies: %v", err)
	}
	if got != second {
		t.Fatalf("overwrite: got %+v, want %+v", got, second)
	}
}
