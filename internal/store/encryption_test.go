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
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// rawValue reads the raw (on-disk) bytes for a key in a given bucket / 读取某 bucket 下某 key 的原始（磁盘）字节.
func rawValue(t *testing.T, s *Store, bucket, key []byte) string {
	t.Helper()
	var out []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		out = append([]byte(nil), tx.Bucket(bucket).Get(key)...)
		return nil
	})
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	return string(out)
}

func TestSystemStateEncryptedAtRest(t *testing.T) {
	s := newTestStore(t)
	in := &SystemState{RabbitMQAdminPassword: "rabbit-secret", RedisPassword: "redis-secret", RedisPort: 6379}
	if err := s.SaveSystemState(in); err != nil {
		t.Fatal(err)
	}

	raw := rawValue(t, s, bucketSystem, []byte("state"))
	if strings.Contains(raw, "rabbit-secret") || strings.Contains(raw, "redis-secret") {
		t.Errorf("plaintext password found on disk: %s", raw)
	}
	if !strings.Contains(raw, "enc:v1:") {
		t.Errorf("expected ciphertext prefix on disk, got: %s", raw)
	}

	got, err := s.GetSystemState()
	if err != nil {
		t.Fatal(err)
	}
	if got.RabbitMQAdminPassword != "rabbit-secret" || got.RedisPassword != "redis-secret" {
		t.Errorf("decrypted state wrong: %+v", got)
	}
	// Non-sensitive fields stay unchanged / 非敏感字段保持不变。
	if got.RedisPort != 6379 {
		t.Errorf("RedisPort = %d, want 6379", got.RedisPort)
	}
}

func TestPluginRuntimeInfoEncryptedAtRest(t *testing.T) {
	s := newTestStore(t)
	in := &PluginInstance{
		ID:     "p1",
		Status: StatusRunning,
		RuntimeInfo: &RuntimeInfo{
			RabbitMQPassword: "amqp-secret",
			RedisPassword:    "redis-secret",
			RabbitMQUser:     "p1user",
		},
	}
	if err := s.SavePlugin(in); err != nil {
		t.Fatal(err)
	}

	raw := rawValue(t, s, bucketPlugins, []byte("p1"))
	if strings.Contains(raw, "amqp-secret") || strings.Contains(raw, "redis-secret") {
		t.Errorf("plaintext password found on disk: %s", raw)
	}
	if !strings.Contains(raw, "enc:v1:") {
		t.Errorf("expected ciphertext prefix on disk, got: %s", raw)
	}
	// The caller's in-memory struct should not be mutated by encryption / 调用方持有的内存结构不应被加密改写。
	if in.RuntimeInfo.RabbitMQPassword != "amqp-secret" {
		t.Errorf("caller struct was mutated: %q", in.RuntimeInfo.RabbitMQPassword)
	}

	got, err := s.GetPlugin("p1")
	if err != nil {
		t.Fatal(err)
	}
	if got.RuntimeInfo.RabbitMQPassword != "amqp-secret" || got.RuntimeInfo.RedisPassword != "redis-secret" {
		t.Errorf("decrypted runtime info wrong: %+v", got.RuntimeInfo)
	}

	list, err := s.ListPlugins()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].RuntimeInfo.RabbitMQPassword != "amqp-secret" {
		t.Errorf("ListPlugins did not decrypt: %+v", list)
	}
}

func TestLegacyPlaintextMigratesOnWrite(t *testing.T) {
	s := newTestStore(t)
	// Write a legacy plaintext instance directly (bypassing the encryption path) / 直接写入历史明文实例（绕过加密路径）。
	legacy := &PluginInstance{ID: "old", Status: StatusStopped, RuntimeInfo: &RuntimeInfo{RabbitMQPassword: "old-plain", RedisPassword: "old-plain2"}}
	data, _ := json.Marshal(legacy)
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPlugins).Put([]byte("old"), data)
	}); err != nil {
		t.Fatal(err)
	}

	// Read should transparently return the plaintext / 读取应透明返回明文。
	got, err := s.GetPlugin("old")
	if err != nil {
		t.Fatal(err)
	}
	if got.RuntimeInfo.RabbitMQPassword != "old-plain" {
		t.Errorf("legacy read = %q, want passthrough", got.RuntimeInfo.RabbitMQPassword)
	}

	// Any write path (here UpdateStatus) should migrate plaintext to ciphertext in-place / 任一写路径应就地迁移为密文。
	if err := s.UpdateStatus("old", StatusRunning); err != nil {
		t.Fatal(err)
	}
	raw := rawValue(t, s, bucketPlugins, []byte("old"))
	if strings.Contains(raw, "old-plain") {
		t.Errorf("legacy plaintext still on disk after write: %s", raw)
	}
	if !strings.Contains(raw, "enc:v1:") {
		t.Errorf("expected migration to ciphertext, got: %s", raw)
	}
}
