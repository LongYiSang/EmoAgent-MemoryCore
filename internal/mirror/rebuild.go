package mirror

import (
	"context"
	"errors"
	"fmt"
)

type Rebuilder struct {
	source    RebuildSource
	adapter   MirrorAdapter
	namespace NamespaceClearer
	indexMap  RebuildIndexMap
}

type RebuilderOptions struct {
	Source    RebuildSource
	Adapter   MirrorAdapter
	Namespace NamespaceClearer
	IndexMap  RebuildIndexMap
}

func NewRebuilder(options RebuilderOptions) *Rebuilder {
	return &Rebuilder{
		source:    options.Source,
		adapter:   options.Adapter,
		namespace: options.Namespace,
		indexMap:  options.IndexMap,
	}
}

func (r *Rebuilder) Rebuild(ctx context.Context, personaID string) (RebuildResult, error) {
	if err := r.validate(); err != nil {
		return RebuildResult{}, err
	}
	if personaID == "" {
		return RebuildResult{}, errors.New("persona_id is required")
	}
	if err := r.namespace.ClearNamespace(ctx, personaID); err != nil {
		return RebuildResult{}, err
	}
	if r.indexMap != nil {
		if err := r.indexMap.MarkPersonaDeleted(ctx, personaID); err != nil {
			return RebuildResult{}, err
		}
	}

	var result RebuildResult
	nodeRefs, err := r.source.ListRebuildNodeRefs(ctx, personaID)
	if err != nil {
		return result, err
	}
	for _, ref := range nodeRefs {
		payload, ok, err := r.source.BuildNodePayload(ctx, ref.PersonaID, ref.NodeType, ref.SQLiteNodeID)
		if err != nil {
			return result, err
		}
		if !ok {
			result.Skipped++
			continue
		}
		upserted, err := r.adapter.UpsertNode(ctx, payload)
		if err != nil {
			result.Failed++
			if r.indexMap != nil {
				if markErr := r.indexMap.MarkNodeFailed(ctx, ref, err.Error()); markErr != nil {
					return result, markErr
				}
			}
			continue
		}
		if r.indexMap != nil {
			if err := r.indexMap.MarkNodeIndexed(ctx, payload, upserted); err != nil {
				return result, err
			}
		}
		result.NodesUpserted++
	}
	if result.Failed > 0 {
		return result, nil
	}

	edgeRefs, err := r.source.ListRebuildEdgeRefs(ctx, personaID)
	if err != nil {
		return result, err
	}
	for _, ref := range edgeRefs {
		payload, ok, err := r.source.BuildEdgePayload(ctx, ref.PersonaID, ref.SQLiteEdgeID)
		if err != nil {
			return result, err
		}
		if !ok {
			result.Skipped++
			continue
		}
		if err := r.adapter.UpsertEdge(ctx, payload); err != nil {
			result.Failed++
			continue
		}
		result.EdgesUpserted++
	}
	return result, nil
}

func (r *Rebuilder) validate() error {
	if r.source == nil {
		return fmt.Errorf("mirror rebuild requires source")
	}
	if r.adapter == nil {
		return fmt.Errorf("mirror rebuild requires adapter")
	}
	if r.namespace == nil {
		return fmt.Errorf("mirror rebuild requires namespace clearer")
	}
	return nil
}
