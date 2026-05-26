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

package auth

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

const sessionTTL = 7 * 24 * time.Hour
const sessionPrefix = "session:"

type SessionData struct {
    UserID   string   `json:"userId"`
    Username string   `json:"username"`
    Role     string   `json:"role"`
}

type SessionManager struct {
    rdb *redis.Client
}

func NewSessionManager(host string, port int, password string) *SessionManager {
    return &SessionManager{
        rdb: redis.NewClient(&redis.Options{
            Addr:     fmt.Sprintf("%s:%d", host, port),
            Password: password,
        }),
    }
}

func (m *SessionManager) Create(ctx context.Context, data SessionData) (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    token := hex.EncodeToString(b)

    payload, err := json.Marshal(data)
    if err != nil {
        return "", err
    }

    key := sessionPrefix + token
    if err := m.rdb.Set(ctx, key, payload, sessionTTL).Err(); err != nil {
        return "", fmt.Errorf("failed to save session: %w", err)
    }

    return token, nil
}

func (m *SessionManager) Get(ctx context.Context, token string) (*SessionData, error) {
    key := sessionPrefix + token
    val, err := m.rdb.Get(ctx, key).Result()
    if err == redis.Nil {
        return nil, nil // token 不存在或已过期
    }
    if err != nil {
        return nil, fmt.Errorf("failed to get session: %w", err)
    }

    var data SessionData
    if err := json.Unmarshal([]byte(val), &data); err != nil {
        return nil, err
    }
    return &data, nil
}

func (m *SessionManager) Delete(ctx context.Context, token string) error {
    return m.rdb.Del(ctx, sessionPrefix+token).Err()
}

// Refresh 续期 token
func (m *SessionManager) Refresh(ctx context.Context, token string) error {
    key := sessionPrefix + token
    return m.rdb.Expire(ctx, key, sessionTTL).Err()
}

func (m *SessionManager) Exists(ctx context.Context, key string) (bool, error) {
    n, err := m.rdb.Exists(ctx, key).Result()
    return n > 0, err
}

func (m *SessionManager) TTL(ctx context.Context, key string) (time.Duration, error) {
    return m.rdb.TTL(ctx, key).Result()
}

func (m *SessionManager) Incr(ctx context.Context, key string) (int64, error) {
    return m.rdb.Incr(ctx, key).Result()
}

func (m *SessionManager) Expire(ctx context.Context, key string, ttl time.Duration) error {
    return m.rdb.Expire(ctx, key, ttl).Err()
}

func (m *SessionManager) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
    return m.rdb.Set(ctx, key, value, ttl).Err()
}

func (m *SessionManager) Del(ctx context.Context, key string) error {
    return m.rdb.Del(ctx, key).Err()
}