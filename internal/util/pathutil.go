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

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidatePluginDir validates a plugin directory path to prevent path traversal attacks / 校验插件目录路径的合法性，防止路径穿越攻击.
//
// Rules:
//  1. Path must be absolute (caller should convert with filepath.Abs first)
//  2. After filepath.Clean, the path must not contain ".." components
//  3. Directory must actually exist
//  4. Path must not point to a system-sensitive directory / 规则：1. 必须绝对路径 2. Clean 后不含 ".." 3. 目录必须存在 4. 不能指向系统敏感目录
func ValidatePluginDir(dir string) error {
	// Must be absolute / 必须是绝对路径
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("plugin directory must be an absolute path, got: %s", dir)
	}

	// Clean to normalize, eliminate . and .. / Clean 规范化，消除 . 和 ..
	clean := filepath.Clean(dir)

	// After normalization, should not contain ".." (theoretically Clean already removes these, but double-check) / 规范化后不应含 ".."，双重保险
	if strings.Contains(clean, "..") {
		return fmt.Errorf("plugin directory contains illegal path component: %s", dir)
	}

	// Reject paths pointing to system-sensitive directories / 拒绝指向系统敏感目录
	sensitive := []string{
		"/etc", "/bin", "/sbin", "/usr/bin", "/usr/sbin",
		"/lib", "/lib64", "/boot", "/sys", "/proc", "/dev",
		"/root", "/var/run", "/run",
	}
	for _, s := range sensitive {
		// Exact or prefix match (prevents both /etc and /etc/passwd from being rejected) / 精确匹配或前缀匹配，防止 /etc 和 /etc/passwd 都被拒绝
		if clean == s || strings.HasPrefix(clean, s+"/") {
			return fmt.Errorf("plugin directory cannot be under system directory %s", s)
		}
	}

	// Directory must exist and be a directory / 目录必须存在且是目录
	info, err := os.Stat(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("plugin directory does not exist: %s", clean)
		}
		return fmt.Errorf("cannot access plugin directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin path is not a directory: %s", clean)
	}

	return nil
}
