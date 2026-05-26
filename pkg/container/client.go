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