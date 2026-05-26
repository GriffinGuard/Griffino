package progress

import (
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	globalProgram *tea.Program
	programMu     sync.RWMutex
)

// Init 启动全局 bubbletea 实例，在 daemon 启动时尽早调用。
// 在独立 goroutine 里运行，不阻塞调用方。
func Init() {
	m := newModel()
	p := tea.NewProgram(m,
		tea.WithMouseCellMotion(),
		tea.WithoutSignalHandler(), // 禁用 bubbletea 自己的 Ctrl+C 处理
	)
	programMu.Lock()
	globalProgram = p
	programMu.Unlock()

	go func() {
		if _, err := p.Run(); err != nil {
		}
	}()
}

// send 发送消息给全局 bubbletea，fallback 到 fmt.Printf。
func send(msg tea.Msg) {
	programMu.RLock()
	p := globalProgram
	programMu.RUnlock()

	if p != nil {
		p.Send(msg)
		return
	}

	// fallback：bubbletea 尚未初始化时直接打印
	switch m := msg.(type) {
	case MsgLog:
		prefix := formatPrefix(m.PluginID)
		switch m.Level {
		case LevelSuccess:
			fmt.Printf("%s✓ %s\n", prefix, m.Text)
		case LevelError:
			fmt.Printf("%s✗ %s\n", prefix, m.Text)
		case LevelWarn:
			fmt.Printf("%s⚠ %s\n", prefix, m.Text)
		default:
			fmt.Printf("%s⟳ %s\n", prefix, m.Text)
		}
	case MsgPullStart:
		fmt.Printf("%s↓ Pulling %s\n", formatPrefix(m.PluginID), m.ImageRef)
	case MsgPullDone:
		fmt.Printf("%s✓ Pulled\n", formatPrefix(m.PluginID))
	}
}

// ── 无状态日志 API ────────────────────────────────────────────────────────────

func Log(pluginID, text string) {
	send(MsgLog{PluginID: pluginID, Level: LevelInfo, Text: text})
}

func Success(pluginID, text string) {
	send(MsgLog{PluginID: pluginID, Level: LevelSuccess, Text: text})
}

func Warn(pluginID, text string) {
	send(MsgLog{PluginID: pluginID, Level: LevelWarn, Text: text})
}

func Error(pluginID, text string) {
	send(MsgLog{PluginID: pluginID, Level: LevelError, Text: text})
}

// ── 进度条 API ────────────────────────────────────────────────────────────────

func PullStart(pluginID, serviceID, imageRef string) {
	send(MsgPullStart{PluginID: pluginID, ServiceID: serviceID, ImageRef: imageRef})
}

func PullLayerUpdate(pluginID, serviceID, layerID string, current, total int64) {
	send(MsgPullLayerUpdate{
		PluginID: pluginID, ServiceID: serviceID,
		LayerID: layerID, Current: current, Total: total,
	})
}

func PullLayerDone(pluginID, serviceID, layerID string) {
	send(MsgPullLayerDone{PluginID: pluginID, ServiceID: serviceID, LayerID: layerID})
}

func PullDone(pluginID, serviceID string) {
	send(MsgPullDone{PluginID: pluginID, ServiceID: serviceID})
}

func Shutdown() {
    programMu.RLock()
    p := globalProgram
    programMu.RUnlock()
    if p == nil {
        return
    }

    time.Sleep(200 * time.Millisecond)
    p.Quit()

    done := make(chan struct{})
    go func() {
        p.Wait()
        close(done)
    }()
    select {
    case <-done:
    case <-time.After(3 * time.Second):
    }
}