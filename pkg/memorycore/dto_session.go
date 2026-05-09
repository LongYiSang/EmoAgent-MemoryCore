package memorycore

import "time"

const (
	ChannelWebUI    = "webui"
	ChannelTelegram = "telegram"
	ChannelQQ       = "qq"
	ChannelCLI      = "cli"
	ChannelAPI      = "api"
	ChannelImported = "imported"
	ChannelOther    = "other"
)

type StartSessionRequest struct {
	ID        string
	PersonaID string
	Channel   string
	Title     *string
	StartedAt time.Time
}

type Session struct {
	ID        string
	PersonaID string
	Channel   string
	Title     *string
	StartedAt time.Time
}
