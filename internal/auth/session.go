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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const sessionPrefix = "session:"
const userSessionsPrefix = "user_sessions:"

// SessionData is stored in Redis for every active token.
type SessionData struct {
	UserID    string    `json:"userId"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionSummary is returned when listing a user's active sessions.
type SessionSummary struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	IsCurrent bool      `json:"isCurrent"`
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

func userSessionsKey(userID string) string { return userSessionsPrefix + userID }

// SessionID returns a stable, non-secret identifier for a bearer token.
func SessionID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create issues a new session token for the given data and stores it with the
// supplied TTL. It also maintains a per-user index (user_sessions:{userID}) so
// sessions can be listed and revoked via ListByUser/Delete.
func (m *SessionManager) Create(ctx context.Context, data SessionData, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	data.CreatedAt = time.Now().UTC()
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	key := sessionPrefix + token
	if err := m.rdb.Set(ctx, key, payload, ttl).Err(); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	// Maintain the per-user index so ListByUser can enumerate tokens.
	indexKey := userSessionsKey(data.UserID)
	m.rdb.SAdd(ctx, indexKey, token)
	m.rdb.Expire(ctx, indexKey, ttl)

	return token, nil
}

func (m *SessionManager) Get(ctx context.Context, token string) (*SessionData, error) {
	key := sessionPrefix + token
	val, err := m.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // token not found or expired / token 不存在或已过期
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

// Delete removes a session and prunes it from the per-user index.
func (m *SessionManager) Delete(ctx context.Context, token string) error {
	// Read the session first to know which user's index to update.
	data, _ := m.Get(ctx, token)
	if data != nil {
		m.rdb.SRem(ctx, userSessionsKey(data.UserID), token)
	}
	return m.rdb.Del(ctx, sessionPrefix+token).Err()
}

// Refresh renews the TTL of an existing session token.
func (m *SessionManager) Refresh(ctx context.Context, token string, ttl time.Duration) error {
	data, err := m.Get(ctx, token)
	if err != nil {
		return err
	}
	key := sessionPrefix + token
	if err := m.rdb.Expire(ctx, key, ttl).Err(); err != nil {
		return err
	}
	if data != nil {
		return m.rdb.Expire(ctx, userSessionsKey(data.UserID), ttl).Err()
	}
	return nil
}

// ListByUser returns all active sessions belonging to the given user. Expired
// entries are pruned from the index as a side effect.
func (m *SessionManager) ListByUser(ctx context.Context, userID string) ([]SessionSummary, error) {
	indexKey := userSessionsKey(userID)
	members, err := m.rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	summaries := make([]SessionSummary, 0, len(members))
	var stale []interface{}

	for _, token := range members {
		data, err := m.Get(ctx, token)
		if err != nil {
			continue // transient Redis error — skip
		}
		if data == nil {
			// Token expired; prune from index lazily.
			stale = append(stale, token)
			continue
		}
		ttlDur, _ := m.rdb.TTL(ctx, sessionPrefix+token).Result()
		summaries = append(summaries, SessionSummary{
			ID:        SessionID(token),
			CreatedAt: data.CreatedAt,
			ExpiresAt: time.Now().UTC().Add(ttlDur),
		})
	}

	if len(stale) > 0 {
		m.rdb.SRem(ctx, indexKey, stale...)
	}

	return summaries, nil
}

// DeleteByUserSessionID deletes a session from a user's session index by its
// public, non-secret session ID.
func (m *SessionManager) DeleteByUserSessionID(ctx context.Context, userID, sessionID string) (bool, error) {
	indexKey := userSessionsKey(userID)
	members, err := m.rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to list sessions: %w", err)
	}
	for _, token := range members {
		if SessionID(token) != sessionID {
			continue
		}
		if err := m.Delete(ctx, token); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
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
