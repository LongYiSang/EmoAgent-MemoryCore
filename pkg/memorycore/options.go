package memorycore

import "time"

type Options struct {
	DBPath      string
	PersonaID   string
	AutoMigrate bool
	EnableFTS   bool
	Now         func() time.Time
}
