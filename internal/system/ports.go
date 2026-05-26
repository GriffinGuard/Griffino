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