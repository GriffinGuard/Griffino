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
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestHandleNodeResultRejectsInvalidOutput(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	s := &Scheduler{taskStore: ts, ctx: ctx}

	bp := &Blueprint{ID: "bp1", Nodes: []Node{{ID: "n1", PluginID: "p1", CapabilityID: "cap1"}}}
	bps := newTestBlueprintStore(t)
	if err := bps.Save(bp); err != nil {
		t.Fatalf("save blueprint: %v", err)
	}
	s.bpStore = bps

	task := &Task{
		ID:          "t1",
		BlueprintID: "bp1",
		UserID:      "u1",
		Status:      TaskStatusRunning,
		Context:     map[string]any{},
		ActiveNodes: map[string]time.Time{"n1": time.Now().Add(time.Minute)},
		CreatedAt:   time.Now(),
	}
	if err := ts.Save(ctx, task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	result := NodeResultEnvelope{
		TaskID:   "t1",
		MsgID:    "m1",
		UserID:   "u1",
		PluginID: "p1",
		NodeID:   "n1",
		Ok:       true,
		Output:   json.RawMessage(`["not","an","object"]`),
	}
	payload, _ := json.Marshal(result)
	err := s.handleNodeResult(DispatchEnvelope{
		TaskID:  "t1",
		MsgID:   "dispatch1",
		UserID:  "u1",
		Event:   "node.result",
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("handleNodeResult: %v", err)
	}

	got, err := ts.Get(ctx, "t1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if got.FailReason == "" {
		t.Fatalf("expected fail reason for invalid output")
	}
}

func newTestBlueprintStore(t *testing.T) *BlueprintStore {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "blueprints.db"), 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketBlueprints)
		return err
	}); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	return NewBlueprintStore(db)
}
