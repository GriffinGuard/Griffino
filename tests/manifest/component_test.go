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

package manifest_test

import (
	"encoding/json"
	"testing"

	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

// A JSON-node-tree root should parse as-is into a WidgetNode tree / JSON 节点树形式应原样解析.
func TestComponentRoot_JSONTree(t *testing.T) {
	raw := `{
		"id": "status",
		"name": {"default": "Status"},
		"refreshMs": 5000,
		"root": {
			"type": "Stack",
			"children": [
				{"type": "Metric", "bind": "count"},
				{"type": "Action", "props": {"actionId": "restart", "label": "Restart"}}
			]
		}
	}`

	var c manifest.Component
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal JSON-tree component: %v", err)
	}
	if c.Root.Node.Type != "Stack" {
		t.Fatalf("root type = %q, want Stack", c.Root.Node.Type)
	}
	if len(c.Root.Node.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(c.Root.Node.Children))
	}
	if got := c.Root.Node.Children[0].Bind; got != "count" {
		t.Fatalf("first child bind = %q, want count", got)
	}
}

// An XML-string root should normalize into an equivalent WidgetNode tree:
// element name->Type, bind attr->Bind, other attrs->Props, inner text->Props["text"].
// XML 字符串形式应规整为等价的 WidgetNode 树。
func TestComponentRoot_XMLString(t *testing.T) {
	raw := `{
		"id": "status",
		"name": {"default": "Status"},
		"root": "<Stack><Text bind=\"title\">Hello</Text><Action actionId=\"restart\" label=\"Restart\"/></Stack>"
	}`

	var c manifest.Component
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal XML-string component: %v", err)
	}
	root := c.Root.Node
	if root.Type != "Stack" || len(root.Children) != 2 {
		t.Fatalf("root = %+v, want Stack with 2 children", root)
	}
	text := root.Children[0]
	if text.Type != "Text" || text.Bind != "title" {
		t.Fatalf("text node = %+v, want Type=Text Bind=title", text)
	}
	if text.Props["text"] != "Hello" {
		t.Fatalf("text inner = %v, want Hello", text.Props["text"])
	}
	action := root.Children[1]
	if action.Type != "Action" || action.Props["actionId"] != "restart" || action.Props["label"] != "Restart" {
		t.Fatalf("action node = %+v, want Action restart/Restart", action)
	}
}

func TestCollectBinds(t *testing.T) {
	root := manifest.WidgetNode{
		Type: "Stack",
		Children: []manifest.WidgetNode{
			{Type: "Metric", Bind: "a"},
			{Type: "Group", Children: []manifest.WidgetNode{
				{Type: "Text", Bind: "b"},
				{Type: "Text", Bind: "a"}, // duplicate, should be deduplicated / 重复，应去重
				{Type: "List", Bind: "*"},
			}},
		},
	}
	got := manifest.CollectBinds(root)
	want := map[string]bool{"a": true, "b": true, "*": true}
	if len(got) != len(want) {
		t.Fatalf("CollectBinds = %v, want 3 unique binds", got)
	}
	for _, b := range got {
		if !want[b] {
			t.Fatalf("unexpected bind %q in %v", b, got)
		}
	}
}

func TestHasAction(t *testing.T) {
	components := []manifest.Component{{
		ID: "panel",
		Root: mustRoot(t, manifest.WidgetNode{
			Type: "Stack",
			Children: []manifest.WidgetNode{
				{Type: "Action", Props: map[string]interface{}{"actionId": "restart"}},
				{Type: "Action", Props: map[string]interface{}{"id": "stop"}}, // falls back to props.id / 退回 props.id
			},
		}),
	}}

	if !manifest.HasAction(components, "restart") {
		t.Error("HasAction(restart) = false, want true")
	}
	if !manifest.HasAction(components, "stop") {
		t.Error("HasAction(stop) = false, want true (props.id fallback)")
	}
	if manifest.HasAction(components, "ghost") {
		t.Error("HasAction(ghost) = true, want false")
	}
}

// mustRoot wraps a WidgetNode into a ComponentRoot (via a JSON round-trip, reusing the
// exported path) / 把 WidgetNode 包成 ComponentRoot.
func mustRoot(t *testing.T, node manifest.WidgetNode) manifest.ComponentRoot {
	t.Helper()
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal node: %v", err)
	}
	var root manifest.ComponentRoot
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}
	return root
}
