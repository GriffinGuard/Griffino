package i18n

import (
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

var localizer *i18n.Localizer

// initLocalizer 根据语言标签初始化全局 localizer，由 Init 调用。
func initLocalizer(lang string) {
	localizer = i18n.NewLocalizer(bundle, lang, "en")
}

// T 翻译一条消息，支持可选的模板变量。
// 用法：
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
		// fallback：直接返回 messageID，不崩溃
		return messageID
	}
	return msg
}