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
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func newTestSchemaStore(t *testing.T) *SchemaStore {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "schemas.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketSchemas)
		return err
	}); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	return NewSchemaStore(db)
}

// TestSeedStandardSchemas verifies the embedded seed parses, loads every standard
// interface, and exposes the expected ports for a representative interface.
func TestSeedStandardSchemas(t *testing.T) {
	store := newTestSchemaStore(t)
	if err := SeedStandardSchemas(store); err != nil {
		t.Fatalf("SeedStandardSchemas: %v", err)
	}

	// Every standard interface in the first-batch set must resolve.
	wantRefs := []string{
		"griffino.interfaces.ai.chat@1.0.0",
		"griffino.interfaces.ai.embedding@1.0.0",
		"griffino.interfaces.ai.speech.transcribe@1.0.0",
		"griffino.interfaces.messaging.notification@1.0.0",
		"griffino.interfaces.web.search@1.0.0",
		"griffino.interfaces.web.scrape@1.0.0",
		"griffino.interfaces.knowledge.vector.upsert@1.0.0",
		"griffino.interfaces.knowledge.vector.query@1.0.0",
		"griffino.interfaces.file.video.process@1.0.0",
	}
	for _, ref := range wantRefs {
		sc, err := store.Get(ref)
		if err != nil {
			t.Errorf("Get(%q): %v", ref, err)
			continue
		}
		if len(sc.InputPorts) == 0 || len(sc.OutputPorts) == 0 {
			t.Errorf("%s: expected non-empty input and output ports, got in=%d out=%d",
				ref, len(sc.InputPorts), len(sc.OutputPorts))
		}
	}

	// Spot-check ai.chat's port shape and types.
	chat, err := store.Get("griffino.interfaces.ai.chat@1.0.0")
	if err != nil {
		t.Fatalf("Get ai.chat: %v", err)
	}
	if got := portByID(chat.InputPorts, "messages"); got == nil || got.Type != "json" || !got.Required {
		t.Errorf("ai.chat input 'messages' = %+v, want type=json required=true", got)
	}
	if got := portByID(chat.OutputPorts, "content"); got == nil || got.Type != "text" {
		t.Errorf("ai.chat output 'content' = %+v, want type=text", got)
	}

	// Seeding twice must be idempotent.
	if err := SeedStandardSchemas(store); err != nil {
		t.Fatalf("SeedStandardSchemas (second run): %v", err)
	}
}

// TestSeedStandardSchemasPortTypesAreCanonical guards that every seeded port uses a
// type from the canonical port-type vocabulary.
func TestSeedStandardSchemasPortTypesAreCanonical(t *testing.T) {
	store := newTestSchemaStore(t)
	if err := SeedStandardSchemas(store); err != nil {
		t.Fatalf("SeedStandardSchemas: %v", err)
	}
	canonical := map[string]bool{
		"text": true, "int": true, "float": true, "bool": true, "json": true,
		"binary": true, "file": true, "image": true, "audio": true, "video": true,
		"embedding": true, "llm-ref": true, "any": true,
	}
	refs := []string{
		"griffino.interfaces.ai.chat@1.0.0",
		"griffino.interfaces.knowledge.vector.query@1.0.0",
		"griffino.interfaces.ai.speech.transcribe@1.0.0",
		"griffino.interfaces.file.video.process@1.0.0",
	}
	for _, ref := range refs {
		sc, err := store.Get(ref)
		if err != nil {
			t.Fatalf("Get(%q): %v", ref, err)
		}
		for _, p := range append(append([]PortSpec{}, sc.InputPorts...), sc.OutputPorts...) {
			if !canonical[p.Type] {
				t.Errorf("%s port %q has non-canonical type %q", ref, p.ID, p.Type)
			}
		}
	}
}

func portByID(ports []PortSpec, id string) *PortSpec {
	for i := range ports {
		if ports[i].ID == id {
			return &ports[i]
		}
	}
	return nil
}
