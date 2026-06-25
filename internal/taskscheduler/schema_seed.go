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

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

// standardSchemaSeed is the embedded port-spec snapshot of the v1 standard interface
// set. It is derived from the Griffino-Schemas repository (the JSON Schema request/
// response pairs) and lists, for each standard interfaceRef, the workflow-facing
// input/output ports — excluding platform-injected fields (userId, jobId) and the
// envelope (ok, error). It should eventually be generated from annotated schemas
// rather than hand-maintained / 嵌入的 v1 标准接口端口快照，源自 Griffino-Schemas.
//
//go:embed schemaseed/standard.json
var standardSchemaSeed []byte

// SeedStandardSchemas loads the embedded v1 standard interface port specs into the
// schema store so StandardInterfaceRef resolves at blueprint-validation time. It is
// idempotent: re-running overwrites each entry with the current seed. The schemas
// bucket must already exist (created during store initialization) /
// 将内嵌标准接口端口规格灌入 schema store，使 StandardInterfaceRef 在蓝图校验时可解析；幂等.
func SeedStandardSchemas(store *SchemaStore) error {
	var schemas []*CachedSchema
	if err := json.Unmarshal(standardSchemaSeed, &schemas); err != nil {
		return fmt.Errorf("parse standard schema seed: %w", err)
	}
	now := time.Now()
	for _, sc := range schemas {
		if sc == nil || sc.InterfaceRef == "" {
			continue
		}
		sc.FetchedAt = now
		if err := store.Save(sc); err != nil {
			return fmt.Errorf("seed schema %s: %w", sc.InterfaceRef, err)
		}
	}
	return nil
}
