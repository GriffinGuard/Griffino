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

package manifest

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ComponentRoot is the container for Component.Root. Plugin authors may write the
// component root node in two ways:
//   - JSON node tree:  "root": { "type": "Stack", "children": [ ... ] }
//   - XML string:      "root": "<Stack><Action actionId=\"restart\" label=\"Restart\"/></Stack>"
//
// Either way, after loading it is normalized into a single WidgetNode (the Node field),
// so handlers only ever see a node tree.
// 组件根节点的容器，支持 JSON 节点树或 XML 字符串两种写法，加载后统一规整为 WidgetNode。
type ComponentRoot struct {
	Node WidgetNode
}

// UnmarshalJSON inspects the first non-whitespace byte: '"' takes the XML-string
// branch, '{' takes the JSON-node branch / 按首字节区分 XML 字符串与 JSON 节点.
func (c *ComponentRoot) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		c.Node = WidgetNode{}
		return nil
	}

	switch trimmed[0] {
	case '"':
		// XML string: unmarshal the string literal first, then parse the XML / 先解出字符串再解析 XML
		var raw string
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			return fmt.Errorf("component root: invalid string: %w", err)
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			c.Node = WidgetNode{}
			return nil
		}
		node, err := parseXMLNode(raw)
		if err != nil {
			return fmt.Errorf("component root: invalid xml: %w", err)
		}
		c.Node = node
		return nil
	case '{':
		var node WidgetNode
		if err := json.Unmarshal(trimmed, &node); err != nil {
			return fmt.Errorf("component root: invalid json node: %w", err)
		}
		c.Node = node
		return nil
	default:
		return fmt.Errorf("component root: expected object or string, got %q", trimmed[0])
	}
}

// MarshalJSON always outputs the JSON node-tree form (XML is normalized at load time),
// so widget endpoints can return it directly / 统一输出 JSON 节点树形式.
func (c ComponentRoot) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Node)
}

// parseXMLNode parses a chunk of XML into a WidgetNode:
//   - element local-name   -> Type
//   - bind attribute        -> Bind
//   - other attributes      -> Props[attr]
//   - child elements         -> Children
//   - non-whitespace text    -> Props["text"]
//
// This mapping matches the node structure the Web-UI renderer expects.
// 把 XML 解析为 WidgetNode，映射与 Web-UI 渲染端一致。
func parseXMLNode(s string) (WidgetNode, error) {
	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return WidgetNode{}, fmt.Errorf("no root element found")
		}
		if err != nil {
			return WidgetNode{}, err
		}
		if start, ok := tok.(xml.StartElement); ok {
			return parseXMLElement(dec, start)
		}
	}
}

func parseXMLElement(dec *xml.Decoder, start xml.StartElement) (WidgetNode, error) {
	node := WidgetNode{Type: start.Name.Local}

	for _, attr := range start.Attr {
		if attr.Name.Local == "bind" {
			node.Bind = attr.Value
			continue
		}
		if node.Props == nil {
			node.Props = map[string]interface{}{}
		}
		node.Props[attr.Name.Local] = attr.Value
	}

	var text strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return WidgetNode{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := parseXMLElement(dec, t)
			if err != nil {
				return WidgetNode{}, err
			}
			node.Children = append(node.Children, child)
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			if inner := strings.TrimSpace(text.String()); inner != "" {
				if node.Props == nil {
					node.Props = map[string]interface{}{}
				}
				if _, exists := node.Props["text"]; !exists {
					node.Props["text"] = inner
				}
			}
			return node, nil
		}
	}
}

// CollectBinds depth-first collects every non-empty bind in the node tree (deduplicated,
// preserving first-seen order). The widget-data handler uses it to decide which Redis
// keys to read / 深度优先收集所有非空 bind，供 widget-data handler 确定要读哪些 key.
func CollectBinds(root WidgetNode) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(n WidgetNode)
	walk = func(n WidgetNode) {
		if n.Bind != "" && !seen[n.Bind] {
			seen[n.Bind] = true
			out = append(out, n.Bind)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// collectActionIDs walks the node trees of a set of Components, collecting the action id
// declared by every type=="Action" node. The action id comes from props.actionId, falling
// back to props.id. Used by the action-trigger endpoint to validate an actionId.
// 收集所有 Action 节点的 action id（props.actionId，其次 props.id），供动作触发接口校验.
func collectActionIDs(components []Component) map[string]bool {
	ids := map[string]bool{}
	var walk func(n WidgetNode)
	walk = func(n WidgetNode) {
		if strings.EqualFold(n.Type, "Action") {
			if id := actionIDFromProps(n.Props); id != "" {
				ids[id] = true
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, c := range components {
		walk(c.Root.Node)
	}
	return ids
}

// HasAction reports whether the given actionID is declared as an <Action> node in any
// component's node tree / 报告 actionID 是否在任一组件树中被声明为 Action 节点.
func HasAction(components []Component, actionID string) bool {
	return collectActionIDs(components)[actionID]
}

func actionIDFromProps(props map[string]interface{}) string {
	if props == nil {
		return ""
	}
	for _, key := range []string{"actionId", "id"} {
		if v, ok := props[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
