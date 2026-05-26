package devdaemon

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client 是 CLI 侧的 socket 客户端。
type Client struct {
	socketPath string
}

// NewClient 创建客户端。socketPath 通常来自 config.SocketPath()。
func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// IsDaemonRunning 检查 daemon 是否在运行（socket 是否可连接）。
func (c *Client) IsDaemonRunning() bool {
	conn, err := net.DialTimeout("unix", c.socketPath, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// DevInstall 通过 daemon 安装 Dev 插件。path 必须是绝对路径。
func (c *Client) DevInstall(path string) (*InstallData, error) {
	payload, _ := json.Marshal(InstallPayload{Path: path})
	resp, err := c.call(Request{Op: OpDevInstall, Payload: payload})
	if err != nil {
		return nil, err
	}
	var data InstallData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &data, nil
}

// DevStart 通过 daemon 启动 Dev 插件。
func (c *Client) DevStart(pluginID string) (*StartData, error) {
	payload, _ := json.Marshal(PluginIDPayload{PluginID: pluginID})
	resp, err := c.call(Request{Op: OpDevStart, Payload: payload})
	if err != nil {
		return nil, err
	}
	var data StartData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &data, nil
}

// DevStop 通过 daemon 停止 Dev 插件。
func (c *Client) DevStop(pluginID string) error {
	payload, _ := json.Marshal(PluginIDPayload{PluginID: pluginID})
	_, err := c.call(Request{Op: OpDevStop, Payload: payload})
	return err
}

// DevUninstall 通过 daemon 卸载 Dev 插件。
// force=true 时先执行 stop 再删除，等价于 dev stop + dev uninstall。
func (c *Client) DevUninstall(pluginID string, force bool) error {
	payload, _ := json.Marshal(UninstallPayload{PluginID: pluginID, Force: force})
	_, err := c.call(Request{Op: OpDevUninstall, Payload: payload})
	return err
}

// call 发送一个请求并读取响应。
func (c *Client) call(req Request) (*Response, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon, is griffino daemon running?: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(60 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return &resp, nil
}