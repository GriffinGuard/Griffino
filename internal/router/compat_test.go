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

package router

import "testing"

func TestInterfaceMajor(t *testing.T) {
	cases := map[string]string{
		"griffino.interfaces.ai.chat@1.0.0": "1",
		"griffino.interfaces.ai.chat@2.3.4": "2",
		"griffino.interfaces.ai.chat@10":    "10",
		"griffino.interfaces.ai.chat":       "",
		"":                                  "",
	}
	for ref, want := range cases {
		if got := interfaceMajor(ref); got != want {
			t.Errorf("interfaceMajor(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestCompatibleProviders(t *testing.T) {
	p := func(id, ref string) ProviderEntry {
		return ProviderEntry{ProviderID: id, InterfaceRef: ref}
	}
	ids := func(ps []ProviderEntry) []string {
		out := make([]string, len(ps))
		for i, x := range ps {
			out[i] = x.ProviderID
		}
		return out
	}
	eq := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}

	t.Run("drops a different major", func(t *testing.T) {
		in := []ProviderEntry{
			p("a", "griffino.interfaces.ai.chat@1.0.0"),
			p("b", "griffino.interfaces.ai.chat@2.0.0"),
			p("c", "griffino.interfaces.ai.chat@1.4.0"),
		}
		if got := ids(compatibleProviders(in)); !eq(got, []string{"a", "c"}) {
			t.Errorf("got %v, want [a c]", got)
		}
	})

	t.Run("keeps untyped providers", func(t *testing.T) {
		in := []ProviderEntry{
			p("a", "griffino.interfaces.ai.chat@1.0.0"),
			p("b", ""),
		}
		if got := ids(compatibleProviders(in)); !eq(got, []string{"a", "b"}) {
			t.Errorf("got %v, want [a b]", got)
		}
	})

	t.Run("all untyped returns all", func(t *testing.T) {
		in := []ProviderEntry{p("a", ""), p("b", "")}
		if got := ids(compatibleProviders(in)); !eq(got, []string{"a", "b"}) {
			t.Errorf("got %v, want [a b]", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := compatibleProviders(nil); len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}
