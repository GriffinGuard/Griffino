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

// Validate 校验三个文件之间的一致性
func Validate(pkg *PluginPackage) error {
    // 1. pluginId 三文件必须一致
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

    // 2. mainServiceId 必须存在于 boot spec 的 services 中
    if _, ok := pkg.BootSpec.Services[pkg.BootSpec.MainServiceID]; !ok {
        return fmt.Errorf(
            "mainServiceId '%s' not found in plugin.boot.yml services",
            pkg.BootSpec.MainServiceID,
        )
    }

    // 3. depends_on 引用的服务必须存在
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

    // 4. config.boot.json 中的 serviceId 必须存在于 boot spec
    for _, svcConfig := range pkg.BootConfig.Services {
        if _, ok := pkg.BootSpec.Services[svcConfig.ID]; !ok {
            return fmt.Errorf(
                "config.boot.json service id '%s' not found in plugin.boot.yml",
                svcConfig.ID,
            )
        }
    }

    // 5. pluginVersion 三文件必须一致
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

    // 6. UserConfig 存在时，pluginId 和 pluginVersion 必须与 manifest 一致
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

    return nil
}