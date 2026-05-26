package container

import (
    "context"
    "fmt"

    "github.com/docker/docker/client"
)

// NewDockerClient 创建 Docker 客户端
func NewDockerClient() (*client.Client, error) {
    cli, err := client.NewClientWithOpts(
        client.FromEnv,
        client.WithAPIVersionNegotiation(),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to create Docker client: %w", err)
    }

    // 验证连接
    ctx := context.Background()
    if _, err := cli.Ping(ctx); err != nil {
        return nil, fmt.Errorf("cannot connect to Docker, is Docker running?: %w", err)
    }

    return cli, nil
}