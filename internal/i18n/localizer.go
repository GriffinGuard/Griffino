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
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

var localizer *i18n.Localizer

// initLocalizer initializes the global localizer based on the language tag; called by Init / 根据语言标签初始化全局 localizer，由 Init 调用.
func initLocalizer(lang string) {
	localizer = i18n.NewLocalizer(bundle, lang, "en")
}

// T translates a message, supporting optional template variables.
// Usage / 翻译一条消息，支持可选的模板变量，用法：
//
//	i18n.T(i18n.MsgPluginStarted)
//	i18n.T(i18n.MsgPluginStarted, map[string]interface{}{"ID": pluginID})
func T(messageID MessageID, templateData ...map[string]interface{}) string {
	cfg := &i18n.LocalizeConfig{
		MessageID: messageID,
	}
	if len(templateData) > 0 && templateData[0] != nil {
		cfg.TemplateData = templateData[0]
	}
	msg, err := localizer.Localize(cfg)
	if err != nil {
		// Fallback: return messageID directly, don't crash / fallback：直接返回 messageID，不崩溃
		return messageID
	}
	return msg
}
