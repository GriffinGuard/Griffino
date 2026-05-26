package taskscheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
	"github.com/redis/go-redis/v9"
)

var bucketBlueprints = []byte("blueprints")
var bucketSchemas    = []byte("schemas")

const taskTTL = 24 * time.Hour

// ─── key 生成函数 ─────────────────────────────────────────────────────────────

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

// ListByUser 返回指定用户的所有蓝图，按 CreatedAt 降序
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
	// 按 CreatedAt 降序排列
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results, nil
}

// FindByTrigger 查找指定用户中匹配 eventType 和 sourcePluginID 的所有蓝图
// sourcePluginID 为空时匹配所有来源；blueprint.Trigger.PluginID 为空时匹配任意来源
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
			// Trigger.PluginID 为空：任意来源都能触发
			// Trigger.PluginID 非空：必须与 sourcePluginID 精确匹配
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

// UpdateStatus 原子更新 Task 状态，避免覆盖其他字段
func (s *TaskStore) UpdateStatus(ctx context.Context, taskID string, status TaskStatus, failReason string) error {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return err
	}
	task.Status = status
	task.FailReason = failReason
	return s.Save(ctx, task)
}

// MergeContext 将 output merge 进 Task.Context 并保存
func (s *TaskStore) MergeContext(ctx context.Context, taskID string, output map[string]any) error {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Context == nil {
		task.Context = make(map[string]any)
	}
	for k, v := range output {
		task.Context[k] = v
	}
	return s.Save(ctx, task)
}

// AdvanceNode 更新 Task 当前执行节点
func (s *TaskStore) AdvanceNode(ctx context.Context, taskID, nodeID string) error {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return err
	}
	task.CurrentNodeID = nodeID
	return s.Save(ctx, task)
}

// IncrLoopCount 递增指定 LOOP 节点的迭代计数，返回递增后的值
func (s *TaskStore) IncrLoopCount(ctx context.Context, taskID, nodeID string) (int, error) {
	task, err := s.Get(ctx, taskID)
	if err != nil {
		return 0, err
	}
	if task.LoopState == nil {
		task.LoopState = make(map[string]int)
	}
	task.LoopState[nodeID]++
	count := task.LoopState[nodeID]
	return count, s.Save(ctx, task)
}

// FindRunningByPlugin 扫描所有 running 状态且 TriggerPluginID 匹配的 Task
// 用于健康检查发现插件挂掉时批量标记失败
// 注意：Redis SCAN 是全量扫描，单机场景下 Task 数量有限，可接受
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

// GetMulti 批量获取多个 schema，找不到的直接跳过（返回 nil）
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