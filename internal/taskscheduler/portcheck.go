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

import "strings"

// ─── Port type compatibility check (W4) ────────────────────────────────────
//
// Blueprints connect nodes via NextNodes edges. For each edge, the upstream output port type must
// satisfy the downstream (required) input port type. Port info comes from CachedSchema,
// pre-resolved by the caller into a nodeID -> *CachedSchema map / 蓝图通过 NextNodes 连边，上游输出端口类型必须满足下游必填输入端口类型
//
// Bypass (no validation) cases:
//   - Either end is a built-in node (input/output/if/loop) — built-in nodes don't declare port schemas;
//   - Either end's schema is unresolved (plugin didn't declare interfaceRef, or schema not yet cached) —
//     no info to validate, prefer allowing over false positives / 放行的情况：任一端是内置节点或 schema 未解析

// PortMismatch describes an incompatible edge / 描述一条不兼容的连边.
type PortMismatch struct {
	FromNodeID string `json:"fromNodeId"`
	ToNodeID   string `json:"toNodeId"`
	PortID     string `json:"portId"`   // Unsatisfied downstream input port ID / 未被满足的下游输入端口 ID
	PortType   string `json:"portType"` // Required type of that input port / 该输入端口要求的类型
	Reason     string `json:"reason"`   // Human-readable reason / 人类可读的原因
}

// typeCompatible checks whether upstream output port type src can serve as downstream input port type dst.
// Rules: equal types are compatible; a small set of implicit upcasts is allowed; "any" and empty types are wildcards / 判断上游输出端口类型 src 是否可作为下游输入端口类型 dst
func typeCompatible(src, dst string) bool {
	s := strings.ToLower(strings.TrimSpace(src))
	d := strings.ToLower(strings.TrimSpace(dst))
	if s == d {
		return true
	}
	// Wildcard types: default or explicit "any" is compatible with any type / 通配类型：缺省或显式 any 与任意类型兼容
	if s == "" || d == "" || s == "any" || d == "any" {
		return true
	}
	// Allowed implicit conversions: src -> {acceptable dst set} / 允许的隐式转换
	conversions := map[string]map[string]bool{
		"int":   {"float": true, "text": true},
		"float": {"text": true},
		"bool":  {"text": true},
	}
	return conversions[s][d]
}

// ValidateBlueprintPorts validates port type compatibility for every edge in the blueprint, returning all mismatches.
// schemas is a caller-resolved nodeID -> cached schema map; built-in nodes and unresolvable schemas
// should be absent from the map (or nil), and their edges will be bypassed / 对蓝图的每条连边做端口类型兼容性校验
func ValidateBlueprintPorts(bp *Blueprint, schemas map[string]*CachedSchema) []PortMismatch {
	if bp == nil {
		return nil
	}

	// nodeID -> *Node, for checking whether a downstream node is built-in / nodeID -> *Node，便于按边查找下游节点是否为内置节点
	nodeByID := make(map[string]*Node, len(bp.Nodes))
	for i := range bp.Nodes {
		nodeByID[bp.Nodes[i].ID] = &bp.Nodes[i]
	}

	var mismatches []PortMismatch
	for i := range bp.Nodes {
		from := &bp.Nodes[i]
		upstream := schemas[from.ID]
		if upstream == nil {
			continue // Built-in node or unresolved schema; bypass all outgoing edges / 内置节点或 schema 未解析，放行该节点所有出边
		}
		for _, toID := range from.NextNodes {
			downstream := schemas[toID]
			if downstream == nil {
				continue // Downstream is built-in or unresolved; bypass / 下游为内置节点或 schema 未解析，放行
			}
			mismatches = append(mismatches,
				checkEdge(from.ID, toID, upstream, downstream)...)
		}
	}
	return mismatches
}

// checkEdge validates a single edge: every required downstream input port must be
// satisfiable by some upstream output port / 校验单条边：下游每个必填输入端口都要能被上游某个输出端口满足.
func checkEdge(fromID, toID string, upstream, downstream *CachedSchema) []PortMismatch {
	var mismatches []PortMismatch
	for _, in := range downstream.InputPorts {
		if !in.Required {
			continue // Optional input ports may be left unconnected / 可选输入端口允许悬空
		}
		if !hasCompatibleOutput(upstream.OutputPorts, in.Type) {
			mismatches = append(mismatches, PortMismatch{
				FromNodeID: fromID,
				ToNodeID:   toID,
				PortID:     in.ID,
				PortType:   in.Type,
				Reason: "no upstream output port produces a value compatible with required input type \"" +
					in.Type + "\"",
			})
		}
	}
	return mismatches
}

// hasCompatibleOutput reports whether any output port in the set can satisfy the target
// input type / 判断输出端口集合中是否存在能满足目标输入类型的端口.
func hasCompatibleOutput(outputs []PortSpec, wantType string) bool {
	for _, out := range outputs {
		if typeCompatible(out.Type, wantType) {
			return true
		}
	}
	return false
}
