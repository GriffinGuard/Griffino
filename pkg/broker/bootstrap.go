package broker

import (
	"fmt"
	"log/slog"
)

// SharedExchanges 系统级共享 exchange，由 daemon 启动时以 admin 身份统一声明
var SharedExchanges = []struct {
	Name    string
	Kind    string
	Durable bool
}{
	{"griffino.plugins", "topic", true}, // 插件间通信
	{"griffino.router",  "topic", true}, // Router 转发
}

// Bootstrap 声明所有系统级共享 exchange，在系统服务就绪后调用一次
func Bootstrap(client *Client, vhost string) error {
    for _, ex := range SharedExchanges {
        slog.Info("declaring shared exchange", "name", ex.Name)
        if err := client.DeclareExchange(vhost, ex.Name, ex.Kind, ex.Durable); err != nil {
            return fmt.Errorf("failed to declare exchange %s: %w", ex.Name, err)
        }
    }
    return nil
}