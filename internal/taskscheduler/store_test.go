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
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestTaskStore(t *testing.T) *TaskStore {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewTaskStore(rdb)
}

func TestTaskStore_ListByUser(t *testing.T) {
	ts := newTestTaskStore(t)
	ctx := context.Background()
	base := time.Now()

	tasks := []*Task{
		{ID: "t1", UserID: "u1", Status: TaskStatusRunning, CreatedAt: base},
		{ID: "t2", UserID: "u1", Status: TaskStatusCompleted, CreatedAt: base.Add(2 * time.Minute)},
		{ID: "t3", UserID: "u2", Status: TaskStatusRunning, CreatedAt: base.Add(time.Minute)},
	}
	for _, tk := range tasks {
		if err := ts.Save(ctx, tk); err != nil {
			t.Fatalf("save %s: %v", tk.ID, err)
		}
	}

	got, err := ts.ListByUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tasks for u1, got %d", len(got))
	}
	// descending: t2 (later) comes first / 降序：t2（较晚）在前
	if got[0].ID != "t2" || got[1].ID != "t1" {
		t.Fatalf("expected CreatedAt desc order [t2,t1], got [%s,%s]", got[0].ID, got[1].ID)
	}
}

func TestTaskStore_ListByUser_Empty(t *testing.T) {
	ts := newTestTaskStore(t)
	got, err := ts.ListByUser(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(got))
	}
}
