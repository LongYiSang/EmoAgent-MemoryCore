package memorycore

import "errors"

var (
	ErrInvalidOptions = errors.New("memorycore: invalid options")
	ErrInvalidRequest = errors.New("memorycore: invalid request")
	ErrNotFound       = errors.New("memorycore: not found")
)
