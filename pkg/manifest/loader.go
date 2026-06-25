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

package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PluginPackage aggregates the three declaration files after loading / 加载完成后三个声明文件的聚合体.
type PluginPackage struct {
	Dir        string
	Manifest   *PluginManifest
	BootConfig *BootConfig
	BootSpec   *BootSpec
	UserConfig *UserConfig
}

// Load loads all declaration files from a plugin directory / 从插件目录加载所有声明文件.
func Load(pluginDir string) (*PluginPackage, error) {
	// 1. load plugin.manifest.json / 加载 plugin.manifest.json
	manifestPath := filepath.Join(pluginDir, "plugin.manifest.json")
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin.manifest.json: %w", err)
	}

	// 2. load the other two files from the paths declared in the manifest / 按 manifest 声明的路径加载另外两个文件
	bootConfigPath := filepath.Join(pluginDir, manifest.ConfigFiles.BootConfig)
	bootConfig, err := loadBootConfig(bootConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s: %w", manifest.ConfigFiles.BootConfig, err)
	}

	bootSpecPath := filepath.Join(pluginDir, manifest.ConfigFiles.RuntimeBoot)
	bootSpec, err := loadBootSpec(bootSpecPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s: %w", manifest.ConfigFiles.RuntimeBoot, err)
	}

	// 3. load config.user.json (optional) / 加载 config.user.json（可选）
	var userConfig *UserConfig
	if manifest.ConfigFiles.UserConfig != "" {
		userConfigPath := filepath.Join(pluginDir, manifest.ConfigFiles.UserConfig)
		userConfig, err = loadUserConfig(userConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load %s: %w", manifest.ConfigFiles.UserConfig, err)
		}
	}

	return &PluginPackage{
		Dir:        pluginDir,
		Manifest:   manifest,
		BootConfig: bootConfig,
		BootSpec:   bootSpec,
		UserConfig: userConfig,
	}, nil
}

func loadManifest(path string) (*PluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m PluginManifest
	// plugin.manifest.json may contain comments; strip them first / manifest 含注释，先去掉
	data = stripJSONComments(data)
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func loadBootConfig(path string) (*BootConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c BootConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func loadBootSpec(path string) (*BootSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s BootSpec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// stripJSONComments removes //-style line comments so the standard library can parse / 去掉 // 行注释.
func stripJSONComments(data []byte) []byte {
	var result []byte
	lines := splitLines(data)
	for _, line := range lines {
		cleaned := removeLineComment(line)
		result = append(result, cleaned...)
		result = append(result, '\n')
	}
	return result
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func removeLineComment(line []byte) []byte {
	inString := false
	for i := 0; i < len(line); i++ {
		if line[i] == '"' {
			inString = !inString
		}
		if !inString && i+1 < len(line) && line[i] == '/' && line[i+1] == '/' {
			return line[:i]
		}
	}
	return line
}

func loadUserConfig(path string) (*UserConfig, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // a missing file is normal / 文件不存在属正常
	}
	if err != nil {
		return nil, err
	}
	var cfg UserConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse user config: %w", err)
	}
	return &cfg, nil
}
