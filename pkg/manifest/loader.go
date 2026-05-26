package manifest

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"

    "gopkg.in/yaml.v3"
)

// PluginPackage 是加载完成后三个文件的聚合体
type PluginPackage struct {
    Dir        string
    Manifest   *PluginManifest
    BootConfig *BootConfig
    BootSpec   *BootSpec
    UserConfig *UserConfig
}

// Load 从插件目录加载所有声明文件
func Load(pluginDir string) (*PluginPackage, error) {
    // 1. 加载 plugin.manifest.json
    manifestPath := filepath.Join(pluginDir, "plugin.manifest.json")
    manifest, err := loadManifest(manifestPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load plugin.manifest.json: %w", err)
    }

    // 2. 根据 manifest 中声明的路径加载另外两个文件
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

    // 3. 加载 config.user.json（可选）
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
    // plugin.manifest.json 含有注释，需要先去掉
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

// stripJSONComments 去掉 // 风格的行注释，使标准库能解析
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
        return nil, nil // 文件不存在是正常的
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