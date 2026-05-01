package core

type MemoryLink struct {
	ID               string
	PersonaID        string
	FromNodeType     NodeType
	FromNodeID       string
	LinkType         LinkType
	ToNodeType       NodeType
	ToNodeID         string
	Direction        LinkDirection
	Confidence       float64
	Weight           float64
	Reasoning        *string
	CreatedBy        LinkCreatedBy
	VisibilityStatus VisibilityStatus
	Searchable       bool
}
