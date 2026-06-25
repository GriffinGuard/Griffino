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

import "testing"

func TestIsLocalOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:5173", true},
		{"http://127.0.0.1:7070", true},
		{"https://localhost", true},
		{"http://[::1]:3000", true},
		{"http://example.com", false},
		{"https://192.168.1.10:7070", false},
		{"http://localhost.evil.com", false},
		{"", false},
		{"not a url", false},
	}
	for _, c := range cases {
		if got := isLocalOrigin(c.origin); got != c.want {
			t.Errorf("isLocalOrigin(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}
