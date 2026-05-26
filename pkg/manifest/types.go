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

// =====================
// plugin.manifest.json
// =====================

type PluginManifest struct {
    ManifestVersion string       `json:"griffinoPluginManifestVersion"`
    ID              string       `json:"id"`
    PluginVersion   string       `json:"pluginVersion"`
    Name            I18nString   `json:"name"`
    Description     I18nString   `json:"description"`
    Author          string       `json:"author"`
    Site            string       `json:"site"`
    Tutorial        string       `json:"tutorial,omitempty"`
    Capabilities    []Capability `json:"capabilities"`
    ConfigFiles     ConfigFiles  `json:"configurationFiles"`
    Permissions     []Permission `json:"permissionsRequested,omitempty"`
    StatusViews     []StatusView `json:"statusViews,omitempty"`
    Actions         []Action     `json:"actions,omitempty"`

}

type I18nString struct {
    Default string `json:"default"`
    I18nKey string `json:"_i18n_key,omitempty"`
}

// String() 让 I18nString 可以直接当字符串用
func (s I18nString) String() string { return s.Default }

type Capability struct {
    ID                     string     `json:"id"`
    Role                   string     `json:"role"` // "provider" | "consumer"
    Name                   I18nString `json:"name"`
    Description            I18nString `json:"description"`
    Type                   string     `json:"type"`
    ConsumesCapabilityType string     `json:"consumesCapabilityType,omitempty"`
    StandardInterfaceRef   string     `json:"standardInterfaceRef,omitempty"`
    EntryPoint             EntryPoint `json:"entryPoint"`
    InputSchemaRef         string     `json:"inputSchemaRef,omitempty"`
    OutputSchemaRef        string     `json:"outputSchemaRef,omitempty"`
    DefaultTimeoutMs       int        `json:"defaultTimeoutMilliseconds,omitempty"`
    Optional               bool       `json:"optional,omitempty"`
    Slots                  []CapabilitySlot `json:"slots,omitempty"`
}

type CapabilitySlot struct {
    ID          string     `json:"id"`
    Name        I18nString `json:"name"`
    Description I18nString `json:"description"`
}

type EntryPoint struct {
    Type    string          `json:"type"`
    Details EntryPointDetails `json:"details"`
}

type EntryPointDetails struct {
    RequestTopicPattern  string `json:"requestTopicPattern"`
    ResponseTopicPattern string `json:"responseTopicPatternHint,omitempty"`
}

type ConfigFiles struct {
    BootConfig  string `json:"bootConfig"`
    RuntimeBoot string `json:"runtimeBoot"`
    UserConfig  string `json:"userConfig,omitempty"`
}

type Permission struct {
    Name        string     `json:"name"`
    Description I18nString `json:"description"`
    Optional    bool       `json:"optional"`
}


// =====================
// plugin.manifest.json — statusViews 字段
// =====================

type StatusViewType string

const (
    StatusViewTypeKV     StatusViewType = "kv"     // 键值对列表
    StatusViewTypeStatus StatusViewType = "status"  // 单个状态标签（带颜色）
)

type StatusView struct {
    ID             string         `json:"id"`
    Name           I18nString     `json:"name"`
    Type           StatusViewType `json:"type"` // "kv" | "status"
    RedisKeyPattern string        `json:"redisKeyPattern"` // 相对于 plugin:{pluginId}:state:{userId}: 的 key 或 pattern
}

// =====================
// plugin.manifest.json — actions 字段
// =====================

type ActionConfirmation struct {
	Required bool       `json:"required"`
	Message  I18nString `json:"message,omitempty"`
}

type Action struct {
	ID           string             `json:"id"`
	Name         I18nString         `json:"name"`
	Description  I18nString         `json:"description"`
	Confirmation ActionConfirmation `json:"confirmation"`
	CooldownMs   int                `json:"cooldownMs,omitempty"` // 默认 3000，前端防抖用
}


// =====================
// config.boot.json
// =====================

type BootConfig struct {
    Version  string          `json:"GriffinoPluginConfigVersion"`
    PluginID string          `json:"pluginId"`
    PluginVersion string     `json:"pluginVersion"`
    Name     string          `json:"name"`
    Site     string          `json:"site"`
    Tutorial string          `json:"tutorial,omitempty"`
    Services []ServiceConfig `json:"services"`
}

type ServiceConfig struct {
    ID      string        `json:"id"`
    Configs []ConfigParam `json:"configs"`
}

type ConfigParam struct {
    Key         string        `json:"key"`
    Name        string        `json:"name"`
    Description string        `json:"description"`
    Type        ConfigType    `json:"type"`
    Optional    bool          `json:"optional"`
    Default     interface{}   `json:"default,omitempty"`
    Values      []OptionValue `json:"values,omitempty"`
    Validation  *Validation   `json:"validation,omitempty"`
    Group       string        `json:"group,omitempty"`
}

type ConfigType string

const (
    ConfigTypeString          ConfigType = "string"
    ConfigTypeInt             ConfigType = "int"
    ConfigTypeFloat           ConfigType = "float"
    ConfigTypeBoolean         ConfigType = "boolean"
    ConfigTypePassword        ConfigType = "password"
    ConfigTypeOptions         ConfigType = "options"
    ConfigTypeMultilineString ConfigType = "multiline_string"
)

type OptionValue struct {
    Value   interface{} `json:"value"`
    Display string      `json:"display"`
}

type Validation struct {
    MinLength *int     `json:"minLength,omitempty"`
    MaxLength *int     `json:"maxLength,omitempty"`
    Pattern   string   `json:"pattern,omitempty"`
    Minimum   *float64 `json:"minimum,omitempty"`
    Maximum   *float64 `json:"maximum,omitempty"`
}

// =====================
// plugin.boot.yml
// =====================

type BootSpec struct {
    SpecVersion   string                 `yaml:"pluginBootSpecVersion"`
    PluginID      string                 `yaml:"pluginId"`
    PluginVersion string                 `yaml:"pluginVersion"`
    MainServiceID string                 `yaml:"mainServiceId"`
    Services      map[string]ServiceSpec `yaml:"services"`
}

type ServiceSpec struct {
    Image             string            `yaml:"image"`
    Environment       []string          `yaml:"environment"`
    Ports             []PortSpec        `yaml:"ports,omitempty"`
    Volumes           []VolumeSpec      `yaml:"volumes,omitempty"`
    DependsOn         []string          `yaml:"depends_on,omitempty"`
    BuildInstructions *BuildInstructions `yaml:"build_instructions,omitempty"`
}

type PortSpec struct {
    Name     string `yaml:"name"`
    Internal int    `yaml:"internal"`
    Protocol string `yaml:"protocol,omitempty"`
}

type VolumeSpec struct {
    Name      string `yaml:"name"`
    MountPath string `yaml:"mount_path"`
    ReadOnly  bool   `yaml:"read_only,omitempty"`
}

type BuildInstructions struct {
    Context    string `yaml:"context"`
    Dockerfile string `yaml:"dockerfile"`
}



// =====================
// config.user.json
// =====================


type UserConfig struct {
    Version       string        `json:"GriffinoPluginConfigVersion"`
    PluginID      string        `json:"pluginId"`
    PluginVersion string        `json:"pluginVersion"`
    Name          string        `json:"name"`
    Site          string        `json:"site"`
    Tutorial      string        `json:"tutorial,omitempty"`
    Configs       []ConfigParam `json:"configs"`
}

