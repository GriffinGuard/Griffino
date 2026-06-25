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

func TestSetupCompletedRoundTrip(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if done, err := s.GetSetupCompleted(); err != nil || done {
		t.Fatalf("fresh db: got (%v, %v), want (false, nil)", done, err)
	}
	if err := s.SetSetupCompleted(true); err != nil {
		t.Fatalf("SetSetupCompleted(true): %v", err)
	}
	if done, err := s.GetSetupCompleted(); err != nil || !done {
		t.Fatalf("after set true: got (%v, %v), want (true, nil)", done, err)
	}
	if err := s.SetSetupCompleted(false); err != nil {
		t.Fatalf("SetSetupCompleted(false): %v", err)
	}
	if done, _ := s.GetSetupCompleted(); done {
		t.Fatalf("after set false: got true, want false")
	}
}
