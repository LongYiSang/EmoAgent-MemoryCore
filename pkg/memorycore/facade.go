package memorycore

import (
	"context"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
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

func ValidateSidecarLoopbackURL(baseURL string) error {
	return internalmirror.ValidateLoopbackURL(baseURL)
}
