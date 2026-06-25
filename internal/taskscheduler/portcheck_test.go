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

package taskscheduler

import "testing"

func TestTypeCompatible(t *testing.T) {
	cases := []struct {
		name     string
		src, dst string
		want     bool
	}{
		{"equal text", "text", "text", true},
		{"equal case insensitive", "Text", "TEXT", true},
		{"int to float", "int", "float", true},
		{"int to text", "int", "text", true},
		{"float to text", "float", "text", true},
		{"bool to text", "bool", "text", true},
		{"float to int rejected", "float", "int", false},
		{"text to int rejected", "text", "int", false},
		{"audio to image rejected", "audio", "image", false},
		{"llm to llm", "llm", "llm", true},
		{"llm to text rejected", "llm", "text", false},
		{"any src wildcard", "any", "image", true},
		{"any dst wildcard", "image", "any", true},
		{"empty src wildcard", "", "image", true},
		{"empty dst wildcard", "image", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := typeCompatible(c.src, c.dst); got != c.want {
				t.Errorf("typeCompatible(%q, %q) = %v, want %v", c.src, c.dst, got, c.want)
			}
		})
	}
}

func TestValidateBlueprintPorts(t *testing.T) {
	// A set of reusable schema constructors / 一组可复用的 schema 构造函数
	out := func(ports ...PortSpec) *CachedSchema { return &CachedSchema{OutputPorts: ports} }
	in := func(ports ...PortSpec) *CachedSchema { return &CachedSchema{InputPorts: ports} }

	textOut := out(PortSpec{ID: "o", Type: "text"})
	intOut := out(PortSpec{ID: "o", Type: "int"})
	audioOut := out(PortSpec{ID: "o", Type: "audio"})

	textInReq := in(PortSpec{ID: "i", Type: "text", Required: true})
	intInReq := in(PortSpec{ID: "i", Type: "int", Required: true})
	textInOpt := in(PortSpec{ID: "i", Type: "text", Required: false})

	cases := []struct {
		name       string
		bp         *Blueprint
		schemas    map[string]*CachedSchema
		wantCount  int
		wantPortID string // First mismatch port ID (checked when wantCount > 0) / 第一个 mismatch 的端口 ID
	}{
		{
			name: "compatible text to text",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas:   map[string]*CachedSchema{"a": textOut, "b": textInReq},
			wantCount: 0,
		},
		{
			name: "incompatible audio to text",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas:    map[string]*CachedSchema{"a": audioOut, "b": textInReq},
			wantCount:  1,
			wantPortID: "i",
		},
		{
			name: "implicit int to text ok",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas:   map[string]*CachedSchema{"a": intOut, "b": textInReq},
			wantCount: 0,
		},
		{
			name: "text cannot satisfy int input",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas:    map[string]*CachedSchema{"a": textOut, "b": intInReq},
			wantCount:  1,
			wantPortID: "i",
		},
		{
			name: "optional input not connected is ok",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas:   map[string]*CachedSchema{"a": audioOut, "b": textInOpt},
			wantCount: 0,
		},
		{
			name: "builtin upstream is skipped",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapInput, NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas:   map[string]*CachedSchema{"b": intInReq}, // a has no schema / a 无 schema
			wantCount: 0,
		},
		{
			name: "builtin downstream output node is skipped",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: BuiltinPluginID, CapabilityID: BuiltinCapOutput},
			}},
			schemas:   map[string]*CachedSchema{"a": audioOut}, // b 无 schema
			wantCount: 0,
		},
		{
			name: "unresolved schema is skipped",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas:   map[string]*CachedSchema{"a": audioOut}, // b 的 schema 未缓存
			wantCount: 0,
		},
		{
			name: "fan out two edges one bad",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b", "c"}},
				{ID: "b", PluginID: "p"},
				{ID: "c", PluginID: "p"},
			}},
			schemas: map[string]*CachedSchema{
				"a": textOut,
				"b": textInReq, // ok
				"c": intInReq,  // bad
			},
			wantCount:  1,
			wantPortID: "i",
		},
		{
			name: "multiple required inputs partly satisfied",
			bp: &Blueprint{Nodes: []Node{
				{ID: "a", PluginID: "p", NextNodes: []string{"b"}},
				{ID: "b", PluginID: "p"},
			}},
			schemas: map[string]*CachedSchema{
				"a": out(PortSpec{ID: "o1", Type: "text"}),
				"b": in(
					PortSpec{ID: "t", Type: "text", Required: true},    // ok
					PortSpec{ID: "img", Type: "image", Required: true}, // bad
				),
			},
			wantCount:  1,
			wantPortID: "img",
		},
		{
			name:      "nil blueprint",
			bp:        nil,
			schemas:   nil,
			wantCount: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ValidateBlueprintPorts(c.bp, c.schemas)
			if len(got) != c.wantCount {
				t.Fatalf("got %d mismatches %+v, want %d", len(got), got, c.wantCount)
			}
			if c.wantCount > 0 && got[0].PortID != c.wantPortID {
				t.Errorf("first mismatch PortID = %q, want %q", got[0].PortID, c.wantPortID)
			}
		})
	}
}
