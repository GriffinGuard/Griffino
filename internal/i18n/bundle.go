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

package i18n

import (
	"embed"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.toml
var localeFS embed.FS

var bundle *i18n.Bundle

// Init 初始化 i18n bundle，在 daemon/CLI 启动时调用一次。
// langOverride 对应 --lang 参数，为空时自动检测系统语言。
func Init(langOverride string) error {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	for _, lang := range []string{"en", "zh_CN"} {
		path := fmt.Sprintf("locales/%s.toml", lang)
		if _, err := bundle.LoadMessageFileFS(localeFS, path); err != nil {
			return fmt.Errorf("failed to load locale file %s: %w", path, err)
		}
	}

	lang := detectLanguage(langOverride)
	initLocalizer(lang)
	return nil
}

// detectLanguage 按优先级检测语言：
// --lang 参数 > GRIFFINO_LANG 环境变量 > 系统 LANG > 默认英文
func detectLanguage(override string) string {
	if override != "" {
		return override
	}
	if env := os.Getenv("GRIFFINO_LANG"); env != "" {
		return env
	}
	if sysLang := os.Getenv("LANG"); sysLang != "" {
		// 系统 LANG 格式通常是 zh_CN.UTF-8，取前缀
		lang := strings.Split(sysLang, ".")[0]
		if lang != "" {
			return lang
		}
	}
	return "en"
}