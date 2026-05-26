package progress

// LogLevel 日志级别
type LogLevel int

const (
	LevelInfo LogLevel = iota
	LevelSuccess
	LevelWarn
	LevelError
)

// 无状态日志消息
type MsgLog struct {
	PluginID string
	Level    LogLevel
	Text     string
}

// 进度条消息
type MsgPullStart struct {
	PluginID  string
	ServiceID string
	ImageRef  string
}

type MsgPullLayerUpdate struct {
	PluginID  string
	ServiceID string
	LayerID   string
	Current   int64
	Total     int64
}

type MsgPullLayerDone struct {
	PluginID  string
	ServiceID string
	LayerID   string
}

type MsgPullDone struct {
	PluginID  string
	ServiceID string
}