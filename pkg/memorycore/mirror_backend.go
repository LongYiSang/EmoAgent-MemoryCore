package memorycore

import (
	"fmt"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

type MirrorBackend interface {
	mirrorBackend()
}

type mirrorBackend struct {
	adapter appcore.MirrorAdapter
}

func (mirrorBackend) mirrorBackend() {}

func NewFakeMirrorBackend() MirrorBackend {
	return mirrorBackend{adapter: appcore.NewFakeMirrorAdapter()}
}

func NewSidecarMirrorBackend(baseURL string) MirrorBackend {
	return mirrorBackend{adapter: appcore.NewSidecarMirrorAdapter(baseURL)}
}

func NewMirrorBackendFromAdapter(adapter any) (MirrorBackend, error) {
	if adapter == nil {
		return nil, nil
	}
	appAdapter, ok := adapter.(appcore.MirrorAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: mirror backend adapter is not supported", ErrInvalidOptions)
	}
	return mirrorBackend{adapter: appAdapter}, nil
}

func toAppMirrorBackend(backend MirrorBackend) appcore.MirrorAdapter {
	if backend == nil {
		return nil
	}
	switch value := backend.(type) {
	case mirrorBackend:
		return value.adapter
	case *mirrorBackend:
		return value.adapter
	default:
		return nil
	}
}
