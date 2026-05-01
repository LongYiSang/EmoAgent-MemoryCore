package core

type MemoryContext struct {
	PersonaID string
	Facts     []Fact
	Blocks    []MemoryContextBlock
}

type MemoryContextBlock struct {
	BlockType     string
	Content       string
	SourceNodeIDs []string
}

type SearchDocument struct {
	ID               string
	PersonaID        string
	NodeType         NodeType
	NodeID           string
	SearchText       string
	SearchTier       SearchTier
	VisibilityStatus VisibilityStatus
	SensitivityLevel SensitivityLevel
	LifecycleStatus  LifecycleStatus
	Searchable       bool
}
