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

import "fmt"

// Validate checks consistency across the three files / 校验三个文件之间的一致性.
func Validate(pkg *PluginPackage) error {
	// 1. pluginId must match across all three files / pluginId 三文件必须一致
	if pkg.BootConfig.PluginID != pkg.Manifest.ID {
		return fmt.Errorf(
			"pluginId mismatch: manifest=%s, config.boot.json=%s",
			pkg.Manifest.ID, pkg.BootConfig.PluginID,
		)
	}
	if pkg.BootSpec.PluginID != pkg.Manifest.ID {
		return fmt.Errorf(
			"pluginId mismatch: manifest=%s, plugin.boot.yml=%s",
			pkg.Manifest.ID, pkg.BootSpec.PluginID,
		)
	}

	// 2. mainServiceId must exist in the boot spec's services / mainServiceId 必须存在于 boot spec 的 services 中
	if _, ok := pkg.BootSpec.Services[pkg.BootSpec.MainServiceID]; !ok {
		return fmt.Errorf(
			"mainServiceId '%s' not found in plugin.boot.yml services",
			pkg.BootSpec.MainServiceID,
		)
	}

	// 3. services referenced by depends_on must exist / depends_on 引用的服务必须存在
	for svcID, svc := range pkg.BootSpec.Services {
		for _, dep := range svc.DependsOn {
			if _, ok := pkg.BootSpec.Services[dep]; !ok {
				return fmt.Errorf(
					"service '%s' depends_on references non-existent service '%s'",
					svcID, dep,
				)
			}
		}
	}

	// 4. serviceId in config.boot.json must exist in the boot spec / config.boot.json 的 serviceId 必须存在于 boot spec
	for _, svcConfig := range pkg.BootConfig.Services {
		if _, ok := pkg.BootSpec.Services[svcConfig.ID]; !ok {
			return fmt.Errorf(
				"config.boot.json service id '%s' not found in plugin.boot.yml",
				svcConfig.ID,
			)
		}
	}

	// 5. pluginVersion must match across all three files / pluginVersion 三文件必须一致
	if pkg.BootConfig.PluginVersion != pkg.Manifest.PluginVersion {
		return fmt.Errorf(
			"pluginVersion mismatch: manifest=%s, config.boot.json=%s",
			pkg.Manifest.PluginVersion, pkg.BootConfig.PluginVersion,
		)
	}
	if pkg.BootSpec.PluginVersion != pkg.Manifest.PluginVersion {
		return fmt.Errorf(
			"pluginVersion mismatch: manifest=%s, plugin.boot.yml=%s",
			pkg.Manifest.PluginVersion, pkg.BootSpec.PluginVersion,
		)
	}

	// 6. when UserConfig is present, its pluginId and pluginVersion must match the manifest / UserConfig 存在时须与 manifest 一致
	if pkg.UserConfig != nil {
		if pkg.UserConfig.PluginID != pkg.Manifest.ID {
			return fmt.Errorf(
				"pluginId mismatch: manifest=%s, config.user.json=%s",
				pkg.Manifest.ID, pkg.UserConfig.PluginID,
			)
		}
		if pkg.UserConfig.PluginVersion != pkg.Manifest.PluginVersion {
			return fmt.Errorf(
				"pluginVersion mismatch: manifest=%s, config.user.json=%s",
				pkg.Manifest.PluginVersion, pkg.UserConfig.PluginVersion,
			)
		}
	}

	// 7. validate ConfigParam type and structure in boot config / 校验 boot config 中 ConfigParam 的类型与结构
	for _, svc := range pkg.BootConfig.Services {
		for i, p := range svc.Configs {
			if err := validateConfigParam(p, fmt.Sprintf("config.boot.json/services/%s/configs[%d]", svc.ID, i)); err != nil {
				return err
			}
		}
	}

	// 8. validate ConfigParam type and structure in user config / 校验 user config 中 ConfigParam 的类型与结构
	if pkg.UserConfig != nil {
		for i, p := range pkg.UserConfig.Configs {
			if err := validateConfigParam(p, fmt.Sprintf("config.user.json/configs[%d]", i)); err != nil {
				return err
			}
		}
	}

	// 9. validate inline interface ports on capabilities / 校验能力的内联接口端口
	for i, cap := range pkg.Manifest.Capabilities {
		if err := validateInlineInterface(cap.InterfaceSpec, fmt.Sprintf("manifest/capabilities[%d] (%q)", i, cap.ID)); err != nil {
			return err
		}
	}

	// 10. validate emitted-event declarations / 校验可发事件声明
	for i, e := range pkg.Manifest.Emits {
		if e.EventType == "" {
			return fmt.Errorf("manifest/emits[%d]: eventType must not be empty", i)
		}
	}

	return nil
}

// validateInlineInterface checks that an inline interface's ports have non-empty IDs
// and use the canonical port-type vocabulary / 校验内联接口端口 ID 非空且类型在词汇内.
func validateInlineInterface(spec *InlineInterfaceSpec, path string) error {
	if spec == nil {
		return nil
	}
	check := func(ports []InterfacePort, kind string) error {
		for i, p := range ports {
			if p.ID == "" {
				return fmt.Errorf("%s.%s[%d]: port id must not be empty", path, kind, i)
			}
			if !IsValidPortType(p.Type) {
				return fmt.Errorf("%s.%s[%d] (%q): unknown port type %q", path, kind, i, p.ID, p.Type)
			}
		}
		return nil
	}
	if err := check(spec.InputPorts, "inputPorts"); err != nil {
		return err
	}
	return check(spec.OutputPorts, "outputPorts")
}

var knownConfigTypes = map[ConfigType]bool{
	ConfigTypeString: true, ConfigTypeInt: true, ConfigTypeFloat: true,
	ConfigTypeBoolean: true, ConfigTypePassword: true, ConfigTypeOptions: true,
	ConfigTypeMultilineString: true, ConfigTypeGroupArray: true,
}

// validateConfigParam checks that a ConfigParam has a recognised type and that
// group_array fields are well-formed (no nested group_array, valid sub-types).
func validateConfigParam(p ConfigParam, path string) error {
	if p.Type != "" && !knownConfigTypes[p.Type] {
		return fmt.Errorf("%s: unknown config type %q", path, p.Type)
	}
	if p.Type == ConfigTypeGroupArray {
		for i, f := range p.Fields {
			if f.Type == ConfigTypeGroupArray {
				return fmt.Errorf("%s.fields[%d] (%q): nested group_array is not supported", path, i, f.Key)
			}
			if err := validateConfigParam(f, fmt.Sprintf("%s.fields[%d]", path, i)); err != nil {
				return err
			}
		}
	} else if len(p.Fields) > 0 {
		return fmt.Errorf("%s: type %q must not have sub-fields", path, p.Type)
	}
	return nil
}
