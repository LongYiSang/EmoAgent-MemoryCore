package memorycore

type RunMirrorSyncRequest struct {
	PersonaID string
	Limit     int
}

type RunMirrorSyncResult struct {
	Claimed   int `json:"claimed"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

type RebuildMirrorRequest struct {
	PersonaID string
}

type RebuildMirrorResult struct {
	NodesUpserted int `json:"nodes_upserted"`
	EdgesUpserted int `json:"edges_upserted"`
	Failed        int `json:"failed"`
	Skipped       int `json:"skipped"`
}
