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

import (
	"sync"
	"sync/atomic"
	"testing"
)

func newTestRouter() *Router {
	return &Router{rrCounters: make(map[string]*atomic.Uint64)}
}

func TestSelectProvider_Empty(t *testing.T) {
	r := newTestRouter()
	if got := r.selectProvider(&Route{Providers: nil}); got != nil {
		t.Fatalf("expected nil for empty providers, got %+v", got)
	}
}

func TestSelectProvider_SingleAlwaysFirst(t *testing.T) {
	r := newTestRouter()
	route := &Route{
		Strategy:  "round_robin",
		Providers: []ProviderEntry{{ProviderID: "a"}},
	}
	for i := 0; i < 5; i++ {
		if got := r.selectProvider(route); got.ProviderID != "a" {
			t.Fatalf("single provider should always return 'a', got %q", got.ProviderID)
		}
	}
}

func TestSelectProvider_FallbackAlwaysFirst(t *testing.T) {
	r := newTestRouter()
	route := &Route{
		Strategy:  "fallback",
		Providers: []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}},
	}
	for i := 0; i < 5; i++ {
		if got := r.selectProvider(route); got.ProviderID != "a" {
			t.Fatalf("fallback should always return first 'a', got %q", got.ProviderID)
		}
	}
}

func TestSelectProvider_RoundRobinEqualWeight(t *testing.T) {
	r := newTestRouter()
	route := &Route{
		PluginID:       "p1",
		CapabilityType: "ai.chat",
		Strategy:       "round_robin",
		Providers:      []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}, {ProviderID: "c"}},
	}
	counts := map[string]int{}
	for i := 0; i < 9; i++ {
		counts[r.selectProvider(route).ProviderID]++
	}
	for _, id := range []string{"a", "b", "c"} {
		if counts[id] != 3 {
			t.Fatalf("expected even 3/3/3 distribution, got %v", counts)
		}
	}
}

func TestSelectProvider_RoundRobinWeighted(t *testing.T) {
	r := newTestRouter()
	route := &Route{
		PluginID:       "p1",
		CapabilityType: "ai.chat",
		Strategy:       "round_robin",
		// weights 3:1 → over 8 requests expect 6 a / 2 b
		Providers: []ProviderEntry{{ProviderID: "a", Weight: 3}, {ProviderID: "b", Weight: 1}},
	}
	counts := map[string]int{}
	for i := 0; i < 8; i++ {
		counts[r.selectProvider(route).ProviderID]++
	}
	if counts["a"] != 6 || counts["b"] != 2 {
		t.Fatalf("expected weighted 6a/2b, got %v", counts)
	}
}

func TestSelectProvider_RoundRobinPerRouteCounters(t *testing.T) {
	r := newTestRouter()
	routeA := &Route{PluginID: "p1", CapabilityType: "ai.chat", Strategy: "round_robin",
		Providers: []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}}}
	routeB := &Route{PluginID: "p2", CapabilityType: "ai.tts", Strategy: "round_robin",
		Providers: []ProviderEntry{{ProviderID: "x"}, {ProviderID: "y"}}}
	// independent counters: each route's first pick is its providers[0]
	if r.selectProvider(routeA).ProviderID != "a" {
		t.Fatal("routeA first pick should be 'a'")
	}
	if r.selectProvider(routeB).ProviderID != "x" {
		t.Fatal("routeB first pick should be 'x' (independent counter)")
	}
}

// fakeHealth treats the specified providerID as unhealthy, all others as healthy / 把指定 providerID 视为不健康，其余视为健康
type fakeHealth struct{ unhealthy map[string]bool }

func (f fakeHealth) IsProviderHealthy(id string) bool { return !f.unhealthy[id] }

func TestSelectProvider_SkipsUnhealthy(t *testing.T) {
	r := newTestRouter()
	r.SetHealthChecker(fakeHealth{unhealthy: map[string]bool{"a": true}})
	route := &Route{
		PluginID:       "p1",
		CapabilityType: "ai.chat",
		Strategy:       "round_robin",
		Providers:      []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}, {ProviderID: "c"}},
	}
	for i := 0; i < 10; i++ {
		if got := r.selectProvider(route); got.ProviderID == "a" {
			t.Fatalf("unhealthy provider 'a' should never be selected")
		}
	}
}

func TestSelectProvider_FallbackSkipsUnhealthyFirst(t *testing.T) {
	r := newTestRouter()
	r.SetHealthChecker(fakeHealth{unhealthy: map[string]bool{"a": true}})
	route := &Route{
		Strategy:  "fallback",
		Providers: []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}},
	}
	// fallback takes the first healthy provider → 'a' is unhealthy, should pick 'b' / fallback 取第一个健康的 → 'a' 不健康，应选 'b'
	if got := r.selectProvider(route); got.ProviderID != "b" {
		t.Fatalf("fallback should skip unhealthy 'a' and pick 'b', got %q", got.ProviderID)
	}
}

func TestSelectProvider_AllUnhealthyFallsBackToAll(t *testing.T) {
	r := newTestRouter()
	r.SetHealthChecker(fakeHealth{unhealthy: map[string]bool{"a": true, "b": true}})
	route := &Route{
		Strategy:  "fallback",
		Providers: []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}},
	}
	// When all are unhealthy, don't silently drop — still return one (the first in the original list) / 全部不健康时不静默丢弃，仍返回一个（原列表第一个）
	if got := r.selectProvider(route); got == nil {
		t.Fatal("expected a provider even when all unhealthy, got nil")
	}
}

func TestSelectProvider_NilHealthCheckerNoFilter(t *testing.T) {
	r := newTestRouter() // health == nil
	route := &Route{
		Strategy:  "fallback",
		Providers: []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}},
	}
	if got := r.selectProvider(route); got.ProviderID != "a" {
		t.Fatalf("with no health checker, fallback returns first 'a', got %q", got.ProviderID)
	}
}

func TestOrderedProviders_FallbackKeepsOrder(t *testing.T) {
	r := newTestRouter()
	route := &Route{
		Strategy:  "fallback",
		Providers: []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}, {ProviderID: "c"}},
	}
	ids := providerIDs(r.orderedProviders(route))
	if len(ids) != 3 || ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Fatalf("fallback should keep original order [a,b,c], got %v", ids)
	}
}

func TestOrderedProviders_FiltersUnhealthy(t *testing.T) {
	r := newTestRouter()
	r.SetHealthChecker(fakeHealth{unhealthy: map[string]bool{"b": true}})
	route := &Route{
		Strategy:  "fallback",
		Providers: []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}, {ProviderID: "c"}},
	}
	ids := providerIDs(r.orderedProviders(route))
	if len(ids) != 2 {
		t.Fatalf("expected 2 healthy providers, got %v", ids)
	}
	for _, id := range ids {
		if id == "b" {
			t.Fatalf("unhealthy 'b' should be excluded, got %v", ids)
		}
	}
}

func TestOrderedProviders_RoundRobinRotatesButKeepsAll(t *testing.T) {
	r := newTestRouter()
	route := &Route{
		PluginID:       "p1",
		CapabilityType: "ai.chat",
		Strategy:       "round_robin",
		Providers:      []ProviderEntry{{ProviderID: "a"}, {ProviderID: "b"}, {ProviderID: "c"}},
	}
	// Each call should return all 3 (as failover candidates), but the starting position rotates / 每次调用应返回全部 3 个（作为失败切换候选），但起始位置轮转
	firsts := map[string]bool{}
	for i := 0; i < 3; i++ {
		ids := providerIDs(r.orderedProviders(route))
		if len(ids) != 3 {
			t.Fatalf("ordered list should contain all 3 providers, got %v", ids)
		}
		set := map[string]bool{}
		for _, id := range ids {
			set[id] = true
		}
		if !set["a"] || !set["b"] || !set["c"] {
			t.Fatalf("ordered list must contain a,b,c, got %v", ids)
		}
		firsts[ids[0]] = true
	}
	if len(firsts) < 2 {
		t.Fatalf("round_robin should rotate the primary across calls, firsts=%v", firsts)
	}
}

func TestOrderedProviders_Empty(t *testing.T) {
	r := newTestRouter()
	if got := r.orderedProviders(&Route{Providers: nil}); got != nil {
		t.Fatalf("expected nil for empty providers, got %v", got)
	}
}

func providerIDs(ps []ProviderEntry) []string {
	ids := make([]string, len(ps))
	for i, p := range ps {
		ids[i] = p.ProviderID
	}
	return ids
}

func TestNextCount_Concurrent(t *testing.T) {
	r := newTestRouter()
	const n = 1000
	var wg sync.WaitGroup
	seen := make([]bool, n)
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := r.nextCount("sig")
			mu.Lock()
			seen[v] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	for i, s := range seen {
		if !s {
			t.Fatalf("nextCount produced duplicate/missing value at %d", i)
		}
	}
}
