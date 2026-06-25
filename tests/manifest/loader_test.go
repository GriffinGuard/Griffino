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

package manifest_test

import (
	"testing"

	"github.com/GriffinGuard/Griffino/pkg/manifest"
)

func TestLoad(t *testing.T) {
	pkg, err := manifest.Load("../../testdata/telegram_bot")
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// verify plugin.manifest.json / 验证 plugin.manifest.json
	t.Run("manifest basic fields", func(t *testing.T) {
		if pkg.Manifest.ID != "cc.griffino.telegram_bot" {
			t.Errorf("want ID=cc.griffino.telegram_bot, got=%s", pkg.Manifest.ID)
		}
		if len(pkg.Manifest.Capabilities) != 4 {
			t.Errorf("want 4 capabilities, got=%d", len(pkg.Manifest.Capabilities))
		}
		if pkg.Manifest.Capabilities[0].ID != "send_notification" {
			t.Errorf("want capability ID=send_notification, got=%s", pkg.Manifest.Capabilities[0].ID)
		}
	})

	// verify config.boot.json / 验证 config.boot.json
	t.Run("config services and params", func(t *testing.T) {
		if len(pkg.BootConfig.Services) != 2 {
			t.Errorf("want 2 services, got=%d", len(pkg.BootConfig.Services))
		}

		// find the telegram-bot-api service / 找到 telegram-bot-api 服务
		var apiService *manifest.ServiceConfig
		for i := range pkg.BootConfig.Services {
			if pkg.BootConfig.Services[i].ID == "telegram-bot-api" {
				apiService = &pkg.BootConfig.Services[i]
			}
		}
		if apiService == nil {
			t.Fatal("telegram-bot-api service config not found")
		}
		if len(apiService.Configs) != 2 {
			t.Errorf("telegram-bot-api: want 2 config items, got=%d", len(apiService.Configs))
		}
	})

	// verify plugin.boot.yml / 验证 plugin.boot.yml
	t.Run("boot spec service definitions", func(t *testing.T) {
		if len(pkg.BootSpec.Services) != 3 {
			t.Errorf("want 3 services, got=%d", len(pkg.BootSpec.Services))
		}
		if pkg.BootSpec.MainServiceID != "bot-app" {
			t.Errorf("want mainServiceId=bot-app, got=%s", pkg.BootSpec.MainServiceID)
		}

		botApp, ok := pkg.BootSpec.Services["bot-app"]
		if !ok {
			t.Fatal("bot-app service definition not found")
		}
		if len(botApp.DependsOn) != 2 {
			t.Errorf("bot-app: want 2 dependencies, got=%d", len(botApp.DependsOn))
		}
	})
}

func TestValidate(t *testing.T) {
	pkg, err := manifest.Load("../../testdata/telegram_bot")
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if err := manifest.Validate(pkg); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
}
