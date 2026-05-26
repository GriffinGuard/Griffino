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

    "go.etcd.io/bbolt"
    "golang.org/x/crypto/bcrypt"
)

var usersBucket = []byte("users")

type UserRole string

const (
    RoleAdmin UserRole = "admin"
    RoleUser  UserRole = "user"
)

type User struct {
    ID           string    `json:"id"`
    Username     string    `json:"username"`
    PasswordHash string    `json:"passwordHash"`
    Role         UserRole  `json:"role"`
    MustChange   bool      `json:"mustChange"`
    Disabled     bool      `json:"disabled"`
    CreatedAt    time.Time `json:"createdAt"`
}

func (s *Store) CreateUser(username, password string, role UserRole, mustChange bool) (*User, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return nil, fmt.Errorf("failed to hash password: %w", err)
    }

    idBytes := make([]byte, 8)
    rand.Read(idBytes)

    user := &User{
        ID:           hex.EncodeToString(idBytes),
        Username:     username,
        PasswordHash: string(hash),
        Role:         role,
        MustChange:   mustChange,
        CreatedAt:    time.Now(),
    }

    return user, s.db.Update(func(tx *bbolt.Tx) error {
        b, err := tx.CreateBucketIfNotExists(usersBucket)
        if err != nil {
            return err
        }
        data, err := json.Marshal(user)
        if err != nil {
            return err
        }
        return b.Put([]byte(username), data)
    })
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
    var user User
    err := s.db.View(func(tx *bbolt.Tx) error {
        b := tx.Bucket(usersBucket)
        if b == nil {
            return nil
        }
        data := b.Get([]byte(username))
        if data == nil {
            return nil
        }
        return json.Unmarshal(data, &user)
    })
    if err != nil {
        return nil, err
    }
    if user.ID == "" {
        return nil, nil
    }
    return &user, nil
}

func (s *Store) UpdateUser(user *User) error {
    return s.db.Update(func(tx *bbolt.Tx) error {
        b, err := tx.CreateBucketIfNotExists(usersBucket)
        if err != nil {
            return err
        }
        data, err := json.Marshal(user)
        if err != nil {
            return err
        }
        return b.Put([]byte(user.Username), data)
    })
}

func (s *Store) HasAnyUser() (bool, error) {
    var has bool
    err := s.db.View(func(tx *bbolt.Tx) error {
        b := tx.Bucket(usersBucket)
        if b == nil {
            return nil
        }
        has = b.Stats().KeyN > 0
        return nil
    })
    return has, err
}

func (s *Store) VerifyPassword(username, password string) (*User, error) {
    user, err := s.GetUserByUsername(username)
    if err != nil || user == nil {
        return nil, fmt.Errorf("invalid username or password")
    }
    if user.Disabled {
        return nil, fmt.Errorf("account is disabled")
    }
    if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
        return nil, fmt.Errorf("invalid username or password")
    }
    return user, nil
}

func (s *Store) ListUsers() ([]*User, error) {
    var users []*User
    err := s.db.View(func(tx *bbolt.Tx) error {
        b := tx.Bucket(usersBucket)
        if b == nil { return nil }
        return b.ForEach(func(k, v []byte) error {
            var u User
            if err := json.Unmarshal(v, &u); err != nil { return err }
            users = append(users, &u)
            return nil
        })
    })
    return users, err
}

func (s *Store) DeleteUser(username string) error {
    return s.db.Update(func(tx *bbolt.Tx) error {
        b := tx.Bucket(usersBucket)
        if b == nil { return nil }
        return b.Delete([]byte(username))
    })
}