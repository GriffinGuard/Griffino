package progress

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleActive  = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleBar     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

const barWidth = 20

type layerState struct {
	current int64
	total   int64
	done    bool
}

type pullState struct {
	imageRef   string
	layers     map[string]*layerState
	layerOrder []string
	done       bool
}

// pluginPullGroup 一个 pluginID 下可能有多个 service 在拉镜像
type pluginPullGroup struct {
	services   map[string]*pullState
	svcOrder   []string
}

type logEntry struct {
	pluginID string
	level    LogLevel
	text     string
}

type Model struct {
	logs      []logEntry
	pulls     map[string]*pluginPullGroup // pluginID -> group
	pullOrder []string                    // 保持 pluginID 顺序
	spinner   spinner.Model
}

func newModel() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styleActive
	return Model{
		pulls:   make(map[string]*pluginPullGroup),
		spinner: s,
	}
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case MsgLog:
		m.logs = append(m.logs, logEntry{
			pluginID: msg.PluginID,
			level:    msg.Level,
			text:     msg.Text,
		})
		return m, nil

	case MsgPullStart:
		if _, exists := m.pulls[msg.PluginID]; !exists {
			m.pullOrder = append(m.pullOrder, msg.PluginID)
			m.pulls[msg.PluginID] = &pluginPullGroup{
				services: make(map[string]*pullState),
			}
		}
		g := m.pulls[msg.PluginID]
		if _, exists := g.services[msg.ServiceID]; !exists {
			g.svcOrder = append(g.svcOrder, msg.ServiceID)
		}
		g.services[msg.ServiceID] = &pullState{
			imageRef: msg.ImageRef,
			layers:   make(map[string]*layerState),
		}
		return m, nil

	case MsgPullLayerUpdate:
		g, ok := m.pulls[msg.PluginID]
		if !ok {
			return m, nil
		}
		ps, ok := g.services[msg.ServiceID]
		if !ok {
			return m, nil
		}
		if _, exists := ps.layers[msg.LayerID]; !exists {
			ps.layerOrder = append(ps.layerOrder, msg.LayerID)
			ps.layers[msg.LayerID] = &layerState{}
		}
		ps.layers[msg.LayerID].current = msg.Current
		ps.layers[msg.LayerID].total = msg.Total
		return m, nil

	case MsgPullLayerDone:
		if g, ok := m.pulls[msg.PluginID]; ok {
			if ps, ok := g.services[msg.ServiceID]; ok {
				if layer, ok := ps.layers[msg.LayerID]; ok {
					layer.done = true
				}
			}
		}
		return m, nil

	case MsgPullDone:
		if g, ok := m.pulls[msg.PluginID]; ok {
			if ps, ok := g.services[msg.ServiceID]; ok {
				ps.done = true
			}
		}
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			p, _ := os.FindProcess(os.Getpid())
        	p.Signal(syscall.SIGINT)
		}
	}

	return m, nil
}

func (m Model) View() string {
	var sb strings.Builder

	// 日志区
	for _, entry := range m.logs {
		prefix := formatPrefix(entry.pluginID)
		switch entry.level {
		case LevelSuccess:
			sb.WriteString(styleSuccess.Render(prefix+"✓ ") + entry.text + "\n")
		case LevelError:
			sb.WriteString(styleError.Render(prefix+"✗ ") + entry.text + "\n")
		case LevelWarn:
			sb.WriteString(styleWarn.Render(prefix+"⚠ ") + entry.text + "\n")
		default:
			sb.WriteString(styleDim.Render(prefix) + "⟳ " + entry.text + "\n")
		}
	}

	// 进度条区
	for _, pluginID := range m.pullOrder {
		g := m.pulls[pluginID]
		prefix := formatPrefix(pluginID)
		for _, svcID := range g.svcOrder {
			ps := g.services[svcID]
			if ps.done {
				sb.WriteString(styleSuccess.Render(fmt.Sprintf("%s✓ Pulled %s\n", prefix, ps.imageRef)))
				continue
			}
			sb.WriteString(styleActive.Render(fmt.Sprintf("%s↓ %s\n", prefix, ps.imageRef)))
			for _, layerID := range ps.layerOrder {
				layer := ps.layers[layerID]
				shortID := layerID
				if len(shortID) > 12 {
					shortID = shortID[:12]
				}
				sb.WriteString(renderLayer(m.spinner, prefix, shortID, layer))
			}
		}
	}

	return sb.String()
}

func formatPrefix(pluginID string) string {
	if pluginID == "" {
		return "[system] "
	}
	return fmt.Sprintf("[%s] ", pluginID)
}

func renderLayer(s spinner.Model, prefix, layerID string, layer *layerState) string {
	p := fmt.Sprintf("%s  [%s] ", prefix, layerID)
	if layer.done {
		return styleSuccess.Render(p+"✓") + "\n"
	}
	if layer.total <= 0 {
		return fmt.Sprintf("%s%s  %s\n", p, s.View(),
			styleDim.Render(formatBytes(layer.current)+" downloaded"))
	}
	pct := float64(layer.current) / float64(layer.total)
	filled := int(pct * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := styleBar.Render(strings.Repeat("█", filled)) +
		styleDim.Render(strings.Repeat("░", barWidth-filled))
	return fmt.Sprintf("%s%s  %3.0f%%  %s / %s\n",
		p, bar, pct*100,
		formatBytes(layer.current), formatBytes(layer.total))
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}