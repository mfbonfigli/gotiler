package model

// RefineMode determines how a tile's content relates to its children in the tileset JSON.
type RefineMode string

const (
	// RefineAdd means a tile's content is additive with its children (children add detail).
	RefineAdd RefineMode = "ADD"
	// RefineReplace means a tile's content replaces its children (children store a copy of parent points).
	RefineReplace RefineMode = "REPLACE"
)
