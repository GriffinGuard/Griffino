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

// ValidatePluginDir 校验插件目录路径的合法性，防止路径穿越攻击。
//
// 规则：
//  1. 路径必须是绝对路径（调用方应先用 filepath.Abs 转换）
//  2. 路径经过 Clean 规范化后不能包含 ".." 组件
//  3. 目录必须实际存在
//  4. 路径不能指向系统敏感目录
func ValidatePluginDir(dir string) error {
	// 必须是绝对路径
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("plugin directory must be an absolute path, got: %s", dir)
	}

	// Clean 规范化，消除 . 和 ..
	clean := filepath.Clean(dir)

	// 规范化后不应含 ".."（理论上 Clean 后绝对路径不会有，但双重保险）
	if strings.Contains(clean, "..") {
		return fmt.Errorf("plugin directory contains illegal path component: %s", dir)
	}

	// 拒绝指向系统敏感目录
	sensitive := []string{
		"/etc", "/bin", "/sbin", "/usr/bin", "/usr/sbin",
		"/lib", "/lib64", "/boot", "/sys", "/proc", "/dev",
		"/root", "/var/run", "/run",
	}
	for _, s := range sensitive {
		// 精确匹配或前缀匹配（防止 /etc 和 /etc/passwd 都被拒绝）
		if clean == s || strings.HasPrefix(clean, s+"/") {
			return fmt.Errorf("plugin directory cannot be under system directory %s", s)
		}
	}

	// 目录必须存在且是目录
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