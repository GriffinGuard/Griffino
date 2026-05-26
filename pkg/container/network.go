package container

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// NetworkName 根据插件ID生成网络名称
func NetworkName(pluginID string) string {
    return "griffino_" + pluginID
}

// CreateNetwork 为插件创建专属的 bridge 网络
func CreateNetwork(ctx context.Context, cli *client.Client, pluginID string) (string, error) {
    name := NetworkName(pluginID)

    // 检查网络是否已存在
    networks, err := cli.NetworkList(ctx, network.ListOptions{
        Filters: filters.NewArgs(filters.Arg("name", name)),
    })
    if err != nil {
        return "", fmt.Errorf("failed to list networks: %w", err)
    }
    for _, n := range networks {
        if n.Name == name {
            return n.ID, nil // 已存在，直接复用
        }
    }

    resp, err := cli.NetworkCreate(ctx, name, network.CreateOptions{
        Driver: "bridge",
        Labels: map[string]string{
            "griffino.plugin.id": pluginID,
            "griffino.managed":   "true",
        },
    })
    if err != nil {
        return "", fmt.Errorf("failed to create network %s: %w", name, err)
    }

    return resp.ID, nil
}

// RemoveNetwork 删除插件的专属网络
func RemoveNetwork(ctx context.Context, cli *client.Client, pluginID string) error {
    name := NetworkName(pluginID)
    if err := cli.NetworkRemove(ctx, name); err != nil {
        return fmt.Errorf("failed to remove network %s: %w", name, err)
    }
    return nil
}