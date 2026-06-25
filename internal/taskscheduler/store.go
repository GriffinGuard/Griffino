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
	"fmt"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
	bolt "go.etcd.io/bbolt"
)

var bucketBlueprints = []byte("blueprints")
var bucketSchemas = []byte("schemas")

const taskTTL = 24 * time.Hour

// ─── Key generation functions ────────────────────────────────────────────────

func taskKey(taskID string) string {
	return fmt.Sprintf("task:%s", taskID)
}

// ─── BlueprintStore（BoltDB）─────────────────────────────────────────────────

type BlueprintStore struct {
	db *bolt.DB
}

func NewBlueprintStore(db *bolt.DB) *BlueprintStore {
	return &BlueprintStore{db: db}
}

func (s *BlueprintStore) Save(bp *Blueprint) error {
	data, err := json.Marshal(bp)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlueprints).Put([]byte(bp.ID), data)
	})
}

func (s *BlueprintStore) Get(id string) (*Blueprint, error) {
	var bp Blueprint
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketBlueprints).Get([]byte(id))
		if data == nil {
			return fmt.Errorf("blueprint not found: %s", id)
		}
		return json.Unmarshal(data, &bp)
	})
	if err != nil {
		return nil, err
	}
	return &bp, nil
}

func (s *BlueprintStore) Delete(id string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlueprints).Delete([]byte(id))
	})
}

// ListByUser returns all blueprints for the given user, ordered by CreatedAt descending / 返回指定用户的所有蓝图，按 CreatedAt 降序
func (s *BlueprintStore) ListByUser(userID string) ([]*Blueprint, error) {
	var results []*Blueprint
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlueprints).ForEach(func(_, v []byte) error {
			var bp Blueprint
			if err := json.Unmarshal(v, &bp); err != nil {
				return err
			}
			if bp.UserID == userID {
				results = append(results, &bp)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	// Ordered by CreatedAt descending / 按 CreatedAt 降序排列
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results, nil
}

// FindByTrigger finds all blueprints matching eventType and sourcePluginID for a given user.
// Empty sourcePluginID matches all sources; empty blueprint.Trigger.PluginID matches any source / 查找指定用户中匹配 eventType 和 sourcePluginID 的所有蓝图
func (s *BlueprintStore) FindByTrigger(userID, eventType, sourcePluginID string) ([]*Blueprint, error) {
	var results []*Blueprint
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlueprints).ForEach(func(_, v []byte) error {
			var bp Blueprint
			if err := json.Unmarshal(v, &bp); err != nil {
				return err
			}
			if bp.UserID != userID || bp.Trigger.EventType != eventType {
				return nil
			}
			// Trigger.PluginID empty: any source can trigger / Trigger.PluginID 为空，任意来源都能触发
			// Trigger.PluginID non-empty: must exactly match sourcePluginID / Trigger.PluginID 非空，必须与 sourcePluginID 精确匹配
			if bp.Trigger.PluginID != "" && bp.Trigger.PluginID != sourcePluginID {
				return nil
			}
			results = append(results, &bp)
			return nil
		})
	})
	return results, err
}

// ─── TaskStore（Redis）───────────────────────────────────────────────────────

type TaskStore struct {
	rdb *redis.Client
}

func NewTaskStore(rdb *redis.Client) *TaskStore {
	return &TaskStore{rdb: rdb}
}

func (s *TaskStore) Save(ctx context.Context, task *Task) error {
	task.UpdatedAt = time.Now()
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, taskKey(task.ID), data, taskTTL).Err()
}

func (s *TaskStore) Get(ctx context.Context, taskID string) (*Task, error) {
	val, err := s.rdb.Get(ctx, taskKey(taskID)).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}
	if err != nil {
		return nil, err
	}
	var task Task
	if err := json.Unmarshal([]byte(val), &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// maxTxRetries is the max retry count on optimistic lock (WATCH/MULTI) conflicts.
// Parallel branches competing for the same Task key can collide; exceeding this returns an error / 乐观锁冲突时的最大重试次数
const maxTxRetries = 100

// update is the atomic workhorse for all Task mutations: uses Redis WATCH/MULTI optimistic locking
// to read Task, apply fn, and write back, auto-retrying on concurrent write conflict (TxFailedErr).
// Solves the TaskStore Get-modify-Save atomicity problem — parallel branch callbacks
// concurrently updating the same Task won't overwrite each other. Returns the latest Task / 所有 Task 变更的原子工作母机
func (s *TaskStore) update(ctx context.Context, taskID string, fn func(*Task) error) (*Task, error) {
	key := taskKey(taskID)
	var result *Task
	txf := func(tx *redis.Tx) error {
		val, err := tx.Get(ctx, key).Result()
		if err == redis.Nil {
			return fmt.Errorf("task not found: %s", taskID)
		}
		if err != nil {
			return err
		}
		var task Task
		if err := json.Unmarshal([]byte(val), &task); err != nil {
			return err
		}
		if err := fn(&task); err != nil {
			return err
		}
		task.UpdatedAt = time.Now()
		data, err := json.Marshal(&task)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, key, data, taskTTL)
			return nil
		})
		if err != nil {
			return err
		}
		result = &task
		return nil
	}

	for i := 0; i < maxTxRetries; i++ {
		err := s.rdb.Watch(ctx, txf, key)
		if err == nil {
			return result, nil
		}
		if err == redis.TxFailedErr {
			continue // Concurrent write invalidated WATCH; retry / 并发写导致 WATCH 失效，重试
		}
		return nil, err
	}
	return nil, fmt.Errorf("task update: max tx retries exceeded for %s", taskID)
}

// UpdateStatus atomically updates Task status without overwriting other fields / 原子更新 Task 状态，避免覆盖其他字段
func (s *TaskStore) UpdateStatus(ctx context.Context, taskID string, status TaskStatus, failReason string) error {
	_, err := s.update(ctx, taskID, func(task *Task) error {
		task.Status = status
		task.FailReason = failReason
		return nil
	})
	return err
}

// MarkTerminal atomically transitions a running Task to a terminal state (completed/failed).
// Returns changed=true only when this call actually performed the running→terminal transition,
// ensuring the callback is sent exactly once when parallel branches concurrently trigger completion/failure / 原子地把 running Task 转为终态，仅当本次完成转换时 changed=true
func (s *TaskStore) MarkTerminal(ctx context.Context, taskID string, status TaskStatus, failReason string) (changed bool, task *Task, err error) {
	task, err = s.update(ctx, taskID, func(t *Task) error {
		changed = false // Re-evaluate on each retry, preventing stale results / 每次重试都重新判定
		if t.Status != TaskStatusRunning {
			return nil // Already terminal; don't modify / 已是终态，本次不改动
		}
		t.Status = status
		t.FailReason = failReason
		changed = true
		return nil
	})
	return changed, task, err
}

// MergeContext merges output into Task.Context and saves / 将 output merge 进 Task.Context 并保存
func (s *TaskStore) MergeContext(ctx context.Context, taskID string, output map[string]any) (*Task, error) {
	return s.update(ctx, taskID, func(task *Task) error {
		if task.Context == nil {
			task.Context = make(map[string]any)
		}
		for k, v := range output {
			task.Context[k] = v
		}
		return nil
	})
}

// AdvanceNode updates the Task's current execution node (best-effort observation field) / 更新 Task 当前执行节点（best-effort 观测字段）
func (s *TaskStore) AdvanceNode(ctx context.Context, taskID, nodeID string) error {
	_, err := s.update(ctx, taskID, func(task *Task) error {
		task.CurrentNodeID = nodeID
		return nil
	})
	return err
}

// AddActiveNode registers an in-flight plugin node and its reply deadline for watchdog timeout detection / 登记一个在途插件节点及其回复截止时间，供看门狗检测超时
func (s *TaskStore) AddActiveNode(ctx context.Context, taskID, nodeID string, deadline time.Time) error {
	_, err := s.update(ctx, taskID, func(task *Task) error {
		if task.ActiveNodes == nil {
			task.ActiveNodes = make(map[string]time.Time)
		}
		task.ActiveNodes[nodeID] = deadline
		task.CurrentNodeID = nodeID
		return nil
	})
	return err
}

// RemoveActiveNode removes an in-flight plugin node upon its completion callback / 在插件节点完成回调时把它从在途集合移除
func (s *TaskStore) RemoveActiveNode(ctx context.Context, taskID, nodeID string) (*Task, error) {
	return s.update(ctx, taskID, func(task *Task) error {
		delete(task.ActiveNodes, nodeID)
		return nil
	})
}

// AddBranches atomically increments the active branch count (delta = N-1 for fan-out to N branches) / 原子地增加存活分支计数
func (s *TaskStore) AddBranches(ctx context.Context, taskID string, delta int) (*Task, error) {
	return s.update(ctx, taskID, func(task *Task) error {
		task.ActiveBranches += delta
		return nil
	})
}

// EndBranch atomically ends a branch (active branches -1), returning the remaining count.
// Zero remaining means this is the last branch; the caller triggers completeTask accordingly / 原子地结束一个分支，返回剩余数，为 0 时触发 completeTask
func (s *TaskStore) EndBranch(ctx context.Context, taskID string) (remaining int, err error) {
	_, err = s.update(ctx, taskID, func(task *Task) error {
		task.ActiveBranches--
		remaining = task.ActiveBranches
		return nil
	})
	return remaining, err
}

// ArriveAtJoin handles a branch arriving at a join node: atomically increments the arrival count.
//   - arrivals < expected: this branch is absorbed (ActiveBranches-1), returns proceed=false;
//   - arrivals >= expected: resets count to 0 (supports loop re-entry), this branch continues as the sole survivor
//     (no branch count decrement), returns proceed=true / 处理分支到达 join 节点，原子自增到达计数
//
// Increment + decision + branch tally in a single atomic transaction, guaranteeing a unique "last arrival" / 自增+判定+分支记账在单次原子事务内完成
func (s *TaskStore) ArriveAtJoin(ctx context.Context, taskID, joinNodeID string, expected int) (proceed bool, err error) {
	_, err = s.update(ctx, taskID, func(task *Task) error {
		if task.JoinState == nil {
			task.JoinState = make(map[string]int)
		}
		arrived := task.JoinState[joinNodeID] + 1
		if arrived >= expected {
			task.JoinState[joinNodeID] = 0 // Reset, supports loop re-entry at join / 复位，支持循环内 join 重入
			proceed = true                 // This branch survives, branch count unchanged / 本分支幸存继续，分支数不变
		} else {
			task.JoinState[joinNodeID] = arrived
			task.ActiveBranches-- // This branch is absorbed / 本分支被吸收
			proceed = false
		}
		return nil
	})
	return proceed, err
}

// IncrLoopCount increments the iteration count for the given LOOP node and returns the new value / 递增指定 LOOP 节点的迭代计数，返回递增后的值
func (s *TaskStore) IncrLoopCount(ctx context.Context, taskID, nodeID string) (int, error) {
	var count int
	_, err := s.update(ctx, taskID, func(task *Task) error {
		if task.LoopState == nil {
			task.LoopState = make(map[string]int)
		}
		task.LoopState[nodeID]++
		count = task.LoopState[nodeID]
		return nil
	})
	return count, err
}

// FindRunningByPlugin scans all running Tasks whose TriggerPluginID matches.
// Used by health check to batch-mark Tasks as failed when a plugin goes down.
// Note: Redis SCAN is a full scan; acceptable for limited single-node Task counts / 扫描所有 running 状态且 TriggerPluginID 匹配的 Task
func (s *TaskStore) FindRunningByPlugin(ctx context.Context, pluginID string) ([]*Task, error) {
	var results []*Task
	iter := s.rdb.Scan(ctx, 0, "task:*", 0).Iterator()
	for iter.Next(ctx) {
		val, err := s.rdb.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var task Task
		if err := json.Unmarshal([]byte(val), &task); err != nil {
			continue
		}
		if task.Status == TaskStatusRunning && task.TriggerPluginID == pluginID {
			results = append(results, &task)
		}
	}
	return results, iter.Err()
}

// ListRunning scans all running Tasks, used by the watchdog for timeout detection / 扫描所有 running 状态的 Task，供看门狗检测超时使用
func (s *TaskStore) ListRunning(ctx context.Context) ([]*Task, error) {
	var results []*Task
	iter := s.rdb.Scan(ctx, 0, "task:*", 0).Iterator()
	for iter.Next(ctx) {
		val, err := s.rdb.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var task Task
		if err := json.Unmarshal([]byte(val), &task); err != nil {
			continue
		}
		if task.Status == TaskStatusRunning {
			results = append(results, &task)
		}
	}
	return results, iter.Err()
}

// ListAll scans all Tasks in Redis, used by admin metrics for cross-user aggregation / 扫描所有 Task，供 admin metrics 跨用户聚合使用
func (s *TaskStore) ListAll(ctx context.Context) ([]*Task, error) {
	var results []*Task
	iter := s.rdb.Scan(ctx, 0, "task:*", 0).Iterator()
	for iter.Next(ctx) {
		val, err := s.rdb.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var task Task
		if err := json.Unmarshal([]byte(val), &task); err != nil {
			continue
		}
		results = append(results, &task)
	}
	return results, iter.Err()
}

// ListByUser scans all Tasks and returns those for the specified user, ordered by CreatedAt descending.
// Note: Redis SCAN is a full scan; acceptable for limited single-node Task counts / 扫描所有 Task，返回指定用户的任务，按 CreatedAt 降序
func (s *TaskStore) ListByUser(ctx context.Context, userID string) ([]*Task, error) {
	var results []*Task
	iter := s.rdb.Scan(ctx, 0, "task:*", 0).Iterator()
	for iter.Next(ctx) {
		val, err := s.rdb.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var task Task
		if err := json.Unmarshal([]byte(val), &task); err != nil {
			continue
		}
		if task.UserID == userID {
			results = append(results, &task)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	return results, nil
}

// ─── SchemaStore（BoltDB）────────────────────────────────────────────────────

type SchemaStore struct {
	db *bolt.DB
}

func NewSchemaStore(db *bolt.DB) *SchemaStore {
	return &SchemaStore{db: db}
}

func (s *SchemaStore) Save(schema *CachedSchema) error {
	data, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSchemas).Put([]byte(schema.InterfaceRef), data)
	})
}

func (s *SchemaStore) Get(interfaceRef string) (*CachedSchema, error) {
	var schema CachedSchema
	err := s.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(bucketSchemas).Get([]byte(interfaceRef))
		if data == nil {
			return fmt.Errorf("schema not found: %s", interfaceRef)
		}
		return json.Unmarshal(data, &schema)
	})
	if err != nil {
		return nil, err
	}
	return &schema, nil
}

// GetMulti batch-loads multiple schemas; missing ones are skipped (returns nil for those) / 批量获取多个 schema，找不到的直接跳过
func (s *SchemaStore) GetMulti(interfaceRefs []string) map[string]*CachedSchema {
	results := make(map[string]*CachedSchema, len(interfaceRefs))
	s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSchemas)
		for _, ref := range interfaceRefs {
			data := b.Get([]byte(ref))
			if data == nil {
				continue
			}
			var schema CachedSchema
			if err := json.Unmarshal(data, &schema); err == nil {
				results[ref] = &schema
			}
		}
		return nil
	})
	return results
}
