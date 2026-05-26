package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Client RabbitMQ Management HTTP API 客户端
type Client struct {
    baseURL    string
    httpClient *http.Client
    adminUser  string
    adminPass  string
}

func NewClient(host string, managementPort int, adminUser, adminPass string) *Client {
    return &Client{
        baseURL:    fmt.Sprintf("http://%s:%d/api", host, managementPort),
        httpClient: &http.Client{},
        adminUser:  adminUser,
        adminPass:  adminPass,
    }
}

func (c *Client) do(method, path string, body interface{}) ([]byte, int, error) {
    var bodyReader io.Reader
    if body != nil {
        data, err := json.Marshal(body)
        if err != nil {
            return nil, 0, err
        }
        bodyReader = bytes.NewReader(data)
    }

    req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
    if err != nil {
        return nil, 0, err
    }
    req.SetBasicAuth(c.adminUser, c.adminPass)
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, 0, fmt.Errorf("failed to call RabbitMQ API: %w", err)
    }
    defer resp.Body.Close()

    data, _ := io.ReadAll(resp.Body)
    return data, resp.StatusCode, nil
}

// HealthCheck 检查 RabbitMQ 是否可达
func (c *Client) HealthCheck() error {
    _, status, err := c.do("GET", "/healthchecks/node", nil)
    if err != nil {
        return err
    }
    if status != http.StatusOK {
        return fmt.Errorf("health check failed, status: %d", status)
    }
    return nil
}

// CreateUser 创建 RabbitMQ 用户
func (c *Client) CreateUser(username, password string) error {
    body := map[string]string{
        "password": password,
        "tags":     "",
    }
    _, status, err := c.do("PUT", "/users/"+username, body)
    if err != nil {
        return err
    }
    if status != http.StatusCreated && status != http.StatusNoContent {
        return fmt.Errorf("failed to create user, status: %d", status)
    }
    return nil
}

// DeleteUser 删除 RabbitMQ 用户
func (c *Client) DeleteUser(username string) error {
    _, status, err := c.do("DELETE", "/users/"+username, nil)
    if err != nil {
        return err
    }
    if status != http.StatusNoContent && status != http.StatusNotFound {
        return fmt.Errorf("failed to delete user, status: %d", status)
    }
    return nil
}

// SetPermissions 设置用户在 vhost 上的权限
func (c *Client) SetPermissions(username, vhost, configure, write, read string) error {
    body := map[string]string{
        "configure": configure,
        "write":     write,
        "read":      read,
    }
	encodedVhost := url.PathEscape(vhost)
    path := fmt.Sprintf("/permissions/%s/%s", encodedVhost, username)
    _, status, err := c.do("PUT", path, body)
    if err != nil {
        return err
    }
    if status != http.StatusCreated && status != http.StatusNoContent {
        return fmt.Errorf("failed to set permissions, status: %d", status)
    }
    return nil
}

// DeclareExchange 声明 exchange
func (c *Client) DeclareExchange(vhost, name, kind string, durable bool) error {
    body := map[string]interface{}{
        "type":        kind,
        "durable":     durable,
        "auto_delete": false,
    }
	encodedVhost := url.PathEscape(vhost)
    path := fmt.Sprintf("/exchanges/%s/%s", encodedVhost, name)
    _, status, err := c.do("PUT", path, body)
    if err != nil {
        return err
    }
    if status != http.StatusCreated && status != http.StatusNoContent {
        return fmt.Errorf("failed to declare exchange, status: %d", status)
    }
    return nil
}

// DeclareQueue 声明 queue
func (c *Client) DeclareQueue(vhost, name string, durable bool) error {
    body := map[string]interface{}{
        "durable":     durable,
        "auto_delete": false,
        "arguments":   map[string]interface{}{},
    }
	encodedVhost := url.PathEscape(vhost)
	path := fmt.Sprintf("/queues/%s/%s", encodedVhost, name)
	_, status, err := c.do("PUT", path, body)
    if err != nil {
        return err
    }
    if status != http.StatusCreated && status != http.StatusNoContent {
        return fmt.Errorf("failed to declare queue, status: %d", status)
    }
    return nil
}

// BindQueue 将 queue 绑定到 exchange
func (c *Client) BindQueue(vhost, queue, exchange, routingKey string) error {
    body := map[string]interface{}{
        "routing_key": routingKey,
        "arguments":   map[string]interface{}{},
    }
	encodedVhost := url.PathEscape(vhost)
    path := fmt.Sprintf("/bindings/%s/e/%s/q/%s", encodedVhost, exchange, queue)
    _, status, err := c.do("POST", path, body)
    if err != nil {
        return err
    }
    if status != http.StatusCreated && status != http.StatusNoContent {
        return fmt.Errorf("failed to bind queue, status: %d", status)
    }
    return nil
}

// SetUserPassword 更新已有用户的密码
func (c *Client) SetUserPassword(username, password string) error {
    body := map[string]string{
        "password": password,
        "tags":     "",
    }
    _, status, err := c.do("PUT", "/users/"+username, body)
    if err != nil {
        return err
    }
    if status != http.StatusCreated && status != http.StatusNoContent {
        return fmt.Errorf("failed to set user password, status: %d", status)
    }
    return nil
}