package memorycore

import (
	"context"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

func Open(ctx context.Context, opts Options) (Service, error) {
	return appcore.Open(ctx, opts)
}

func NewFakeMirrorAdapter() MirrorAdapter {
	return appcore.NewFakeMirrorAdapter()
}

func NewSidecarMirrorAdapter(baseURL string) MirrorAdapter {
	return appcore.NewSidecarMirrorAdapter(baseURL)
}
