package manifest_test

import (
    "testing"
    
    "github.com/GriffinGuard/Griffino/pkg/manifest"
)

func TestLoad(t *testing.T) {
    pkg, err := manifest.Load("../../testdata/telegram_bot")
    if err != nil {
        t.Fatalf("Load() 失败: %v", err)
    }

    // 验证 plugin.manifest.json
    t.Run("manifest基本字段", func(t *testing.T) {
        if pkg.Manifest.ID != "cc.griffino.telegram_bot" {
            t.Errorf("期望 ID=cc.griffino.telegram_bot, 实际=%s", pkg.Manifest.ID)
        }
        if len(pkg.Manifest.Capabilities) != 1 {
            t.Errorf("期望 1 个 capability, 实际=%d", len(pkg.Manifest.Capabilities))
        }
        if pkg.Manifest.Capabilities[0].ID != "send_notification" {
            t.Errorf("期望 capability ID=send_notification, 实际=%s", pkg.Manifest.Capabilities[0].ID)
        }
    })

    // 验证 config.boot.json
    t.Run("config服务和参数", func(t *testing.T) {
        if len(pkg.BootConfig.Services) != 2 {
            t.Errorf("期望 2 个 service, 实际=%d", len(pkg.BootConfig.Services))
        }

        // 找到 telegram-bot-api 服务
        var apiService *manifest.ServiceConfig
        for i := range pkg.BootConfig.Services {
            if pkg.BootConfig.Services[i].ID == "telegram-bot-api" {
                apiService = &pkg.BootConfig.Services[i]
            }
        }
        if apiService == nil {
            t.Fatal("找不到 telegram-bot-api 服务配置")
        }
        if len(apiService.Configs) != 2 {
            t.Errorf("telegram-bot-api 期望 2 个配置项, 实际=%d", len(apiService.Configs))
        }
    })

    // 验证 plugin.boot.yml
    t.Run("boot spec服务定义", func(t *testing.T) {
        if len(pkg.BootSpec.Services) != 3 {
            t.Errorf("期望 3 个 service, 实际=%d", len(pkg.BootSpec.Services))
        }
        if pkg.BootSpec.MainServiceID != "bot-app" {
            t.Errorf("期望 mainServiceId=bot-app, 实际=%s", pkg.BootSpec.MainServiceID)
        }

        botApp, ok := pkg.BootSpec.Services["bot-app"]
        if !ok {
            t.Fatal("找不到 bot-app 服务定义")
        }
        if len(botApp.DependsOn) != 2 {
            t.Errorf("bot-app 期望 2 个依赖, 实际=%d", len(botApp.DependsOn))
        }
    })
}

func TestValidate(t *testing.T) {
    pkg, err := manifest.Load("../../testdata/telegram_bot")
    if err != nil {
        t.Fatalf("Load() 失败: %v", err)
    }

    if err := manifest.Validate(pkg); err != nil {
        t.Fatalf("Validate() 失败: %v", err)
    }
}