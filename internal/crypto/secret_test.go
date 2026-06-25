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

package crypto

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := NewFromKeyFile(filepath.Join(t.TempDir(), "secret.key"))
	if err != nil {
		t.Fatalf("NewFromKeyFile: %v", err)
	}
	return c
}

func TestEncryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	for _, plain := range []string{"hunter2", "a", "包含中文与符号!@#"} { // test data includes CJK; not a translatable comment
		ct, err := c.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt: %v", err)
		}
		if !IsEncrypted(ct) {
			t.Errorf("ciphertext %q lacks prefix", ct)
		}
		if ct == plain {
			t.Errorf("ciphertext equals plaintext for %q", plain)
		}
		got, err := c.Decrypt(ct)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if got != plain {
			t.Errorf("round-trip = %q, want %q", got, plain)
		}
	}
}

func TestEncryptEmptyPassthrough(t *testing.T) {
	c := newTestCipher(t)
	ct, err := c.Encrypt("")
	if err != nil || ct != "" {
		t.Errorf("Encrypt(\"\") = %q, %v; want empty", ct, err)
	}
}

func TestDecryptLegacyPlaintextPassthrough(t *testing.T) {
	c := newTestCipher(t)
	// No prefix → treat as legacy plaintext, return as-is (transparent migration) / 无前缀视为历史明文，原样返回（透明迁移）。
	got, err := c.Decrypt("legacy-plaintext")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "legacy-plaintext" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestEncryptIdempotent(t *testing.T) {
	c := newTestCipher(t)
	once, _ := c.Encrypt("secret")
	twice, err := c.Encrypt(once)
	if err != nil {
		t.Fatalf("Encrypt(ciphertext): %v", err)
	}
	if twice != once {
		t.Errorf("re-encrypting ciphertext changed it: %q != %q", twice, once)
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	a := newTestCipher(t)
	b := newTestCipher(t) // Different TempDir → different key / 不同 TempDir → 不同密钥
	ct, _ := a.Encrypt("secret")
	if _, err := b.Decrypt(ct); err == nil {
		t.Error("expected decrypt with wrong key to fail")
	}
}

func TestLoadOrCreateKeyStableAndPermissioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "secret.key")
	k1, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(k1) != keySize {
		t.Fatalf("key size %d, want %d", len(k1), keySize)
	}
	k2, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if string(k1) != string(k2) {
		t.Error("key changed across loads")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("key file perm = %o, want 0600", perm)
		}
	}
}
