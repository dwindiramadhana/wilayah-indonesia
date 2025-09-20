package model

// SearchRequest captures query parameters from HTTP callers.
type SearchRequest struct {
	Query       string
	Subdistrict string
	District    string
	City        string
	Province    string
	Options     SearchOptions
}

// SearchOptions fine-tunes search behaviour and enrichment.
type SearchOptions struct {
	Limit         int
	SearchBPS     bool
	IncludeBPS    bool
	IncludeScores bool
}
