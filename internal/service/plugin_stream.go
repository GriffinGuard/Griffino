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

package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/GriffinGuard/Griffino/internal/logger"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// streamPluginLogs 持续读取单个容器的日志，直到 ctx 取消或容器退出。
// 由 startCore 成功后以 goroutine 方式调用。
func streamPluginLogs(ctx context.Context, dockerCli *client.Client, pluginID, serviceID, containerName string) {
	pl, err := logger.GetPluginLogger(pluginID)
	if err != nil {
		slog.Warn("failed to get plugin logger", "plugin", pluginID, "error", err)
		return
	}

	rc, err := dockerCli.ContainerLogs(ctx, containerName, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err != nil {
		slog.Warn("failed to open container log stream",
			"plugin", pluginID, "service", serviceID, "container", containerName, "error", err)
		return
	}
	defer rc.Close()

	// Docker 的 stdout/stderr 是多路复用在同一个流里的，需要用 stdcopy 分离
	pr, pw := io.Pipe()
	errPr, errPw := io.Pipe()

	go func() {
		_, _ = stdcopy.StdCopy(pw, errPw, rc)
		pw.Close()
		errPw.Close()
	}()

	// 分别处理 stdout 和 stderr
	go processLines(ctx, pl, pluginID, serviceID, errPr, true)
	processLines(ctx, pl, pluginID, serviceID, pr, false)
}

// processLines 逐行读取流，写入对应日志文件。
// isStderr=true 时无前缀的行默认视为 WARN。
func processLines(ctx context.Context, pl *logger.PluginLogger, pluginID, serviceID string, r io.Reader, isStderr bool) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 256)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				idx := strings.Index(string(buf), "\n")
				if idx < 0 {
					break
				}
				line := strings.TrimRight(string(buf[:idx]), "\r")
				buf = buf[idx+1:]
				writeLine(pl, pluginID, serviceID, line, isStderr)
			}
		}
		if err != nil {
			return
		}
	}
}

// writeLine 解析日志级别并写入对应文件。
func writeLine(pl *logger.PluginLogger, pluginID, serviceID, line string, isStderr bool) {
	ts := time.Now().UTC().Format(time.RFC3339)

	level := "INFO"
	if isStderr {
		level = "WARN"
	}
	// 识别插件 SDK 约定的前缀
	upper := strings.ToUpper(line)
	if strings.HasPrefix(upper, "[INFO]") {
		level = "INFO"
		line = strings.TrimSpace(line[6:])
	} else if strings.HasPrefix(upper, "[WARN]") {
		level = "WARN"
		line = strings.TrimSpace(line[6:])
	} else if strings.HasPrefix(upper, "[ERROR]") {
		level = "ERROR"
		line = strings.TrimSpace(line[7:])
	}

	formatted := fmt.Sprintf("%s [%s] [%s] [%s] %s\n", ts, pluginID, serviceID, level, line)

	_, _ = pl.All.Write([]byte(formatted))

	if level == "WARN" || level == "ERROR" {
		_, _ = pl.Err.Write([]byte(formatted))
		if gw := logger.GlobalPluginErrWriter(); gw != nil {
			_, _ = gw.Write([]byte(formatted))
		}
	}
}