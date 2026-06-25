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
	Components      []Component  `json:"components,omitempty"`
	// Emits advertises the event types this plugin can emit as a trigger (event
	// source). The workflow engine matches blueprints by these event types /
	// 声明本插件作为触发器可发出的事件类型，供蓝图发现与订阅.
	Emits []EmittedEvent `json:"emits,omitempty"`
}

// EmittedEvent declares one event type a trigger plugin can emit. Keep this shape
// in sync with the plugin SDKs' trigger declaration / 触发器可发事件声明，与 SDK 同步.
type EmittedEvent struct {
	EventType   string     `json:"eventType"`
	SchemaRef   string     `json:"schemaRef,omitempty"`
	Name        I18nString `json:"name,omitempty"`
	Description I18nString `json:"description,omitempty"`
}

type I18nString struct {
	Default string `json:"default"`
	I18nKey string `json:"_i18n_key,omitempty"`
}

// String lets I18nString be used directly as a string / 让 I18nString 可直接当字符串用.
func (s I18nString) String() string { return s.Default }

type Capability struct {
	ID                     string     `json:"id"`
	Role                   string     `json:"role"` // "provider" | "consumer"
	Name                   I18nString `json:"name"`
	Description            I18nString `json:"description"`
	Type                   string     `json:"type"`
	ConsumesCapabilityType string     `json:"consumesCapabilityType,omitempty"`
	StandardInterfaceRef   string     `json:"standardInterfaceRef,omitempty"`
	// InterfaceSpec declares the capability's port schema inline, used when
	// StandardInterfaceRef is empty (a custom, non-standard interface). It lets a
	// capability participate in workflow port validation without being a registered
	// standard. Keep this shape in sync with the plugin SDKs / 内联自定义接口端口规格.
	InterfaceSpec    *InlineInterfaceSpec `json:"interfaceSpec,omitempty"`
	EntryPoint       EntryPoint           `json:"entryPoint"`
	DefaultTimeoutMs int                  `json:"defaultTimeoutMilliseconds,omitempty"`
	Optional         bool                 `json:"optional,omitempty"`
	Slots            []CapabilitySlot     `json:"slots,omitempty"`
}

// InlineInterfaceSpec is an inline port schema for a custom capability / 内联端口规格.
type InlineInterfaceSpec struct {
	InputPorts  []InterfacePort `json:"inputPorts,omitempty"`
	OutputPorts []InterfacePort `json:"outputPorts,omitempty"`
}

// InterfacePort describes one workflow data port of a capability. Type must be a
// member of the canonical port-type vocabulary (see IsValidPortType) / 能力的一个数据端口.
type InterfacePort struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

type CapabilitySlot struct {
	ID          string     `json:"id"`
	Name        I18nString `json:"name"`
	Description I18nString `json:"description"`
}

type EntryPoint struct {
	Type    string            `json:"type"`
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
// plugin.manifest.json — components field
// =====================
//
// Component is the carrier that unifies the three former concepts of Widgets /
// Components / Actions: a host-rendered "component" declared by a plugin. It both
// displays information (display nodes) and offers quick actions (Action nodes) —
// quick actions are no longer a separate top-level concept but a type=="Action" node
// inside the component's node tree.
//
// Root is a renderer-agnostic node tree (WidgetNode); the host maps each node's type to
// a primitive in its own whitelist: the Web-UI maps to React components, and a future
// mobile app can map the same tree to native controls. The backend stays type-agnostic,
// only carrying this tree and collecting its binds, which keeps it portable.
// Component 统一了旧的 Widgets/Components/Actions：与渲染端无关的 WidgetNode 树，后端保持 type-无关以保证可移植性。

type Component struct {
	ID        string        `json:"id"`
	Name      I18nString    `json:"name"`
	RefreshMs int           `json:"refreshMs,omitempty"`
	Root      ComponentRoot `json:"root"` // either a JSON node tree or an XML string / JSON 节点树或 XML 字符串
}

// WidgetNode is a single node in the component tree. type selects the render primitive;
// bind resolves to live data; control-flow nodes (When/Match/Repeat, etc.) render
// children conditionally / 组件树中的单个节点：type 选渲染原语，bind 取实时数据.
type WidgetNode struct {
	Type     string                 `json:"type"`
	Props    map[string]interface{} `json:"props,omitempty"`
	Bind     string                 `json:"bind,omitempty"`
	Children []WidgetNode           `json:"children,omitempty"`
}

// =====================
// config.boot.json
// =====================

type BootConfig struct {
	Version       string          `json:"GriffinoPluginConfigVersion"`
	PluginID      string          `json:"pluginId"`
	PluginVersion string          `json:"pluginVersion"`
	Name          string          `json:"name"`
	Site          string          `json:"site"`
	Tutorial      string          `json:"tutorial,omitempty"`
	Services      []ServiceConfig `json:"services"`
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
	Fields      []ConfigParam `json:"fields,omitempty"`
	MinItems    *int          `json:"minItems,omitempty"`
	MaxItems    *int          `json:"maxItems,omitempty"`
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
	ConfigTypeGroupArray      ConfigType = "group_array"
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
	Image             string             `yaml:"image"`
	Environment       []string           `yaml:"environment"`
	Ports             []PortSpec         `yaml:"ports,omitempty"`
	Volumes           []VolumeSpec       `yaml:"volumes,omitempty"`
	DependsOn         []string           `yaml:"depends_on,omitempty"`
	BuildInstructions *BuildInstructions `yaml:"build_instructions,omitempty"`
	// Resources caps the container's memory/CPU/PIDs. Optional; unset fields fall
	// back to the platform defaults so no plugin runs unbounded / 容器资源上限，缺省回退平台默认.
	Resources *ResourceLimits `yaml:"resources,omitempty"`
}

// ResourceLimits declares per-service container resource caps / 单服务容器资源上限.
type ResourceLimits struct {
	MemoryMB  int     `yaml:"memory_mb,omitempty"`  // hard memory limit in MiB
	CPUs      float64 `yaml:"cpus,omitempty"`       // CPU limit, e.g. 0.5 = half a core
	PidsLimit int     `yaml:"pids_limit,omitempty"` // max number of processes/threads
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
