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

package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var bucketAuditLogs = []byte("audit_logs")

// AuditLog records a single administrative or user action for compliance and debugging / 记录单次管理或用户操作，用于合规与调试
type AuditLog struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Detail    string    `json:"detail,omitempty"`
	Level     string    `json:"level"` // "info" | "warning" | "error"
}

// CreateAuditLog persists an audit log entry. The key is <UnixNano>_<random-hex> so
// BoltDB byte-order sort yields chronological order / 持久化一条审计日志；key 为时间戳_随机 hex，BoltDB 字节序即时间序
func (s *Store) CreateAuditLog(entry *AuditLog) error {
	if entry.ID == "" {
		idBytes := make([]byte, 8)
		rand.Read(idBytes)
		entry.ID = hex.EncodeToString(idBytes)
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.Level == "" {
		entry.Level = "info"
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal audit log: %w", err)
	}

	// Key: <nanoseconds>_<id> — zero-padded to 19 digits to preserve sort order / key 零填充 19 位保持排序
	key := fmt.Sprintf("%019d_%s", entry.Timestamp.UnixNano(), entry.ID)

	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketAuditLogs)
		if err != nil {
			return err
		}
		return b.Put([]byte(key), data)
	})
}

// AuditFilter holds optional filter criteria for ListAuditLogs / 审计日志查询过滤条件
type AuditFilter struct {
	Actor    string
	Action   string
	Resource string
	From     time.Time
	To       time.Time
}

// ListAuditLogs returns a paginated, filtered list of audit log entries in reverse-chronological order.
// page and pageSize are 1-based; pageSize ≤ 0 defaults to 20 / 返回分页过滤后的审计日志，逆序；page 从 1 起，pageSize ≤ 0 默认 20
func (s *Store) ListAuditLogs(f AuditFilter, page, pageSize int) ([]*AuditLog, int, error) {
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	if page <= 0 {
		page = 1
	}

	var all []*AuditLog
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketAuditLogs)
		if b == nil {
			return nil
		}
		// Iterate in reverse (newest first) / 逆序遍历
		c := b.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var entry AuditLog
			if err := json.Unmarshal(v, &entry); err != nil {
				continue
			}
			if f.Actor != "" && entry.Actor != f.Actor {
				continue
			}
			if f.Action != "" && entry.Action != f.Action {
				continue
			}
			if f.Resource != "" && entry.Resource != f.Resource {
				continue
			}
			if !f.From.IsZero() && entry.Timestamp.Before(f.From) {
				continue
			}
			if !f.To.IsZero() && entry.Timestamp.After(f.To) {
				continue
			}
			all = append(all, &entry)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	total := len(all)
	start := (page - 1) * pageSize
	if start >= total {
		return []*AuditLog{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}
