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

package system

import (
    "fmt"
    "net"
)

// findAvailablePort 从 startPort 开始向上找一个未被占用的端口
func findAvailablePort(startPort int) (int, error) {
    for port := startPort; port < startPort+100; port++ {
        addr := fmt.Sprintf("127.0.0.1:%d", port)
        ln, err := net.Listen("tcp", addr)
        if err != nil {
            continue // 端口被占用，试下一个
        }
        ln.Close()
        return port, nil
    }
	return 0, fmt.Errorf("no available port found in range %d-%d", startPort, startPort+100)
}