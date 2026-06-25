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

import "testing"

// validPkg returns a minimal plugin package that passes the cross-file consistency
// checks, so individual tests can exercise one new validation rule in isolation.
func validPkg() *PluginPackage {
	const id, ver = "com.test.x", "1.0.0"
	return &PluginPackage{
		Manifest:   &PluginManifest{ID: id, PluginVersion: ver},
		BootConfig: &BootConfig{PluginID: id, PluginVersion: ver},
		BootSpec: &BootSpec{
			PluginID:      id,
			PluginVersion: ver,
			MainServiceID: "svc",
			Services:      map[string]ServiceSpec{"svc": {Image: "img"}},
		},
	}
}

func TestIsValidPortType(t *testing.T) {
	for _, ok := range []string{
		"text", "int", "float", "bool", "json", "binary",
		"file", "image", "audio", "video", "embedding", "llm-ref", "any",
	} {
		if !IsValidPortType(ok) {
			t.Errorf("IsValidPortType(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "string", "number", "vector", "TEXT", "boolean"} {
		if IsValidPortType(bad) {
			t.Errorf("IsValidPortType(%q) = true, want false", bad)
		}
	}
}

func TestValidateInlineInterfaceOK(t *testing.T) {
	pkg := validPkg()
	pkg.Manifest.Capabilities = []Capability{{
		ID:   "c1",
		Role: "provider",
		Type: "com.test.x.do",
		InterfaceSpec: &InlineInterfaceSpec{
			InputPorts:  []InterfacePort{{ID: "in", Type: PortText, Required: true}},
			OutputPorts: []InterfacePort{{ID: "out", Type: PortJSON}},
		},
	}}
	if err := Validate(pkg); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateInlineInterfaceRejectsUnknownType(t *testing.T) {
	pkg := validPkg()
	pkg.Manifest.Capabilities = []Capability{{
		ID: "c1",
		InterfaceSpec: &InlineInterfaceSpec{
			InputPorts: []InterfacePort{{ID: "in", Type: "frobnicate"}},
		},
	}}
	if err := Validate(pkg); err == nil {
		t.Fatal("expected error for unknown port type, got nil")
	}
}

func TestValidateInlineInterfaceRejectsEmptyPortID(t *testing.T) {
	pkg := validPkg()
	pkg.Manifest.Capabilities = []Capability{{
		ID: "c1",
		InterfaceSpec: &InlineInterfaceSpec{
			OutputPorts: []InterfacePort{{ID: "", Type: PortText}},
		},
	}}
	if err := Validate(pkg); err == nil {
		t.Fatal("expected error for empty port id, got nil")
	}
}

func TestValidateEmitsOK(t *testing.T) {
	pkg := validPkg()
	pkg.Manifest.Emits = []EmittedEvent{{
		EventType: "griffino.events.rss.item",
		SchemaRef: "griffino.events.rss.item@1.0.0",
	}}
	if err := Validate(pkg); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateEmitsRejectsEmptyEventType(t *testing.T) {
	pkg := validPkg()
	pkg.Manifest.Emits = []EmittedEvent{{EventType: ""}}
	if err := Validate(pkg); err == nil {
		t.Fatal("expected error for empty emits eventType, got nil")
	}
}
