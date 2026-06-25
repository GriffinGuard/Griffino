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
	"fmt"
	"strconv"

	bolt "go.etcd.io/bbolt"
)

// bucketSettings holds global, single-row server settings (created in New()).
var bucketSettings = []byte("settings")

const keySetupCompleted = "setup_completed"

// GetSetupCompleted reports whether the first-run setup wizard has been
// completed. It is a global, server-side flag (distinct from a per-user
// must-change-password), so the wizard does not reappear across browsers.
func (s *Store) GetSetupCompleted() (bool, error) {
	var done bool
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSettings)
		if b == nil {
			return nil
		}
		done = string(b.Get([]byte(keySetupCompleted))) == "true"
		return nil
	})
	return done, err
}

// SetSetupCompleted records whether first-run setup has been completed.
func (s *Store) SetSetupCompleted(completed bool) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketSettings)
		if err != nil {
			return err
		}
		val := []byte("false")
		if completed {
			val = []byte("true")
		}
		return b.Put([]byte(keySetupCompleted), val)
	})
}

// SecurityPolicies holds server-wide security thresholds that admins can tune
// via the Web UI. All fields have conservative defaults matching the previous
// hardcoded constants.
type SecurityPolicies struct {
	// SessionTTLHours is how long a session token stays valid (default 168 = 7 days).
	SessionTTLHours int `json:"sessionTtlHours"`
	// MaxLoginAttempts is the number of consecutive failures before lockout (default 5).
	MaxLoginAttempts int `json:"maxLoginAttempts"`
	// LockoutDurationMinutes is how long the account is locked after too many failures (default 15).
	LockoutDurationMinutes int `json:"lockoutDurationMinutes"`
}

const (
	keySessionTTLHours        = "session_ttl_hours"
	keyMaxLoginAttempts       = "max_login_attempts"
	keyLockoutDurationMinutes = "lockout_duration_minutes"
)

func defaultSecurityPolicies() SecurityPolicies {
	return SecurityPolicies{
		SessionTTLHours:        168,
		MaxLoginAttempts:       5,
		LockoutDurationMinutes: 15,
	}
}

// GetSecurityPolicies reads the current security policies from the settings
// bucket. Missing keys fall back to their defaults so the struct is always
// fully populated.
func (s *Store) GetSecurityPolicies() (SecurityPolicies, error) {
	p := defaultSecurityPolicies()
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSettings)
		if b == nil {
			return nil
		}
		if v := b.Get([]byte(keySessionTTLHours)); len(v) > 0 {
			if n, err := strconv.Atoi(string(v)); err == nil {
				p.SessionTTLHours = n
			}
		}
		if v := b.Get([]byte(keyMaxLoginAttempts)); len(v) > 0 {
			if n, err := strconv.Atoi(string(v)); err == nil {
				p.MaxLoginAttempts = n
			}
		}
		if v := b.Get([]byte(keyLockoutDurationMinutes)); len(v) > 0 {
			if n, err := strconv.Atoi(string(v)); err == nil {
				p.LockoutDurationMinutes = n
			}
		}
		return nil
	})
	return p, err
}

// ─── SMTP configuration ──────────────────────────────────────────────────────

// SMTPConfig holds SMTP server settings. Password is stored encrypted; GET responses omit it / SMTP 服务器配置，密码加密存储，GET 响应不返回密码
type SMTPConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password"` // encrypted at rest / 静态加密
	FromEmail  string `json:"fromEmail"`
	Encryption string `json:"encryption"` // "none" | "ssl" | "tls"
	Configured bool   `json:"configured"`
}

const keySMTPConfig = "smtp_config"

// GetSMTPConfig reads SMTP configuration. Password is decrypted before return / 读取 SMTP 配置，密码解密后返回
func (s *Store) GetSMTPConfig() (SMTPConfig, error) {
	var cfg SMTPConfig
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSettings)
		if b == nil {
			return nil
		}
		v := b.Get([]byte(keySMTPConfig))
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, &cfg)
	})
	if err != nil {
		return SMTPConfig{}, err
	}
	if cfg.Password != "" {
		plain, err := s.cipher.Decrypt(cfg.Password)
		if err != nil {
			return SMTPConfig{}, fmt.Errorf("decrypt smtp password: %w", err)
		}
		cfg.Password = plain
	}
	return cfg, nil
}

// SetSMTPConfig persists SMTP configuration. Password is encrypted before storage / 持久化 SMTP 配置，密码加密后存储
func (s *Store) SetSMTPConfig(cfg SMTPConfig) error {
	toStore := cfg
	if cfg.Password != "" {
		enc, err := s.cipher.Encrypt(cfg.Password)
		if err != nil {
			return fmt.Errorf("encrypt smtp password: %w", err)
		}
		toStore.Password = enc
	}
	toStore.Configured = true
	data, err := json.Marshal(toStore)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketSettings)
		if err != nil {
			return err
		}
		return b.Put([]byte(keySMTPConfig), data)
	})
}

// SetSecurityPolicies persists all three security policy fields atomically.
func (s *Store) SetSecurityPolicies(p SecurityPolicies) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketSettings)
		if err != nil {
			return err
		}
		if err := b.Put([]byte(keySessionTTLHours), []byte(strconv.Itoa(p.SessionTTLHours))); err != nil {
			return err
		}
		if err := b.Put([]byte(keyMaxLoginAttempts), []byte(strconv.Itoa(p.MaxLoginAttempts))); err != nil {
			return err
		}
		return b.Put([]byte(keyLockoutDurationMinutes), []byte(strconv.Itoa(p.LockoutDurationMinutes)))
	})
}
