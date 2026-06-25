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

package service

import (
	"testing"

	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

func TestMissingRequiredConfig(t *testing.T) {
	cfg := func(params ...manifest.ConfigParam) *manifest.BootConfig {
		return &manifest.BootConfig{
			Services: []manifest.ServiceConfig{{ID: "main", Configs: params}},
		}
	}
	required := manifest.ConfigParam{Key: "token", Type: manifest.ConfigTypeString}                  // Required, no default / 必填，无默认
	withDefault := manifest.ConfigParam{Key: "lang", Type: manifest.ConfigTypeString, Default: "en"} // Has default -> compatible / 有默认 -> 兼容
	optional := manifest.ConfigParam{Key: "note", Type: manifest.ConfigTypeString, Optional: true}   // Optional -> compatible / 可选 -> 兼容

	cases := []struct {
		name   string
		admin  map[string]map[string]string
		newCfg *manifest.BootConfig
		want   bool
	}{
		{"nil config is compatible", nil, nil, false},
		{"new required key missing -> incompatible", map[string]map[string]string{}, cfg(required), true},
		{"new required key already provided -> compatible",
			map[string]map[string]string{"main": {"token": "abc"}}, cfg(required), false},
		{"empty value counts as missing -> incompatible",
			map[string]map[string]string{"main": {"token": ""}}, cfg(required), true},
		{"required-but-has-default -> compatible", map[string]map[string]string{}, cfg(withDefault), false},
		{"optional -> compatible", map[string]map[string]string{}, cfg(optional), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := missingRequiredConfig(c.admin, c.newCfg); got != c.want {
				t.Fatalf("missingRequiredConfig = %v, want %v", got, c.want)
			}
		})
	}
}
