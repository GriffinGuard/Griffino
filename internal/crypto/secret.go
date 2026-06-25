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

// Package crypto provides at-rest encryption for sensitive store fields. The master key is persisted
// in a local secret.key file (mode 0600), using AES-256-GCM. Ciphertext carries an "enc:v1:" prefix
// so encrypted and legacy plaintext values can be distinguished unambiguously / 提供存储层敏感字段的 at-rest 加密，密文带前缀以区分明文.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ciphertextPrefix marks a value as encrypted by this package. Values without this prefix are treated as legacy plaintext / 标记加密产物，无前缀视为历史明文.
const ciphertextPrefix = "enc:v1:"

const keySize = 32 // AES-256

// Cipher performs AES-256-GCM encryption/decryption with a symmetric key / 用对称密钥做 AES-256-GCM 加解密.
type Cipher struct {
	aead cipher.AEAD
}

// LoadOrCreateKey reads a 32-byte master key from path; generates one with CSPRNG and writes it with mode 0600 if it doesn't exist / 读取主密钥，不存在则用 CSPRNG 生成并写入.
func LoadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != keySize {
			return nil, fmt.Errorf("secret key %s has wrong size %d (want %d)", path, len(data), keySize)
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read secret key: %w", err)
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, fmt.Errorf("write secret key: %w", err)
	}
	return key, nil
}

// NewCipher creates a Cipher from a 32-byte key / 用 32 字节密钥构造 Cipher.
func NewCipher(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// NewFromKeyFile is a convenience wrapper for LoadOrCreateKey + NewCipher / LoadOrCreateKey + NewCipher 的便捷封装.
func NewFromKeyFile(path string) (*Cipher, error) {
	key, err := LoadOrCreateKey(path)
	if err != nil {
		return nil, err
	}
	return NewCipher(key)
}

// Encrypt encrypts plaintext and returns a prefixed ciphertext. Empty string is returned as-is;
// already-encrypted values are also returned as-is (idempotent, safe to call on a read-modify-write path) / 加密并返回带前缀的密文，空串和已加密值原样返回（幂等）.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" || IsEncrypted(plaintext) {
		return plaintext, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return ciphertextPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decrypts a prefixed ciphertext. Values without the prefix are treated as legacy plaintext and returned as-is (transparent migration) / 解密带前缀的密文，无前缀视为历史明文原样返回.
func (c *Cipher) Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(value[len(ciphertextPrefix):])
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(raw) < c.aead.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether a value is a ciphertext produced by this package / 报告 value 是否为本包的密文.
func IsEncrypted(value string) bool {
	return len(value) >= len(ciphertextPrefix) && value[:len(ciphertextPrefix)] == ciphertextPrefix
}
