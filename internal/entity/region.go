package entity

// Region represents a persisted Indonesian administrative record.
type Region struct {
	ID          string     `json:"id"`
	Subdistrict string     `json:"subdistrict"`
	District    string     `json:"district"`
	City        string     `json:"city"`
	Province    string     `json:"province"`
	PostalCode  string     `json:"postal_code"`
	FullText    string     `json:"full_text"`
	BPS         *RegionBPS `json:"bps,omitempty"`
}

// RegionBPS stores optional BPS naming and codes for each administrative level.
type RegionBPS struct {
	Subdistrict *BPSDetail `json:"subdistrict,omitempty"`
	District    *BPSDetail `json:"district,omitempty"`
	City        *BPSDetail `json:"city,omitempty"`
	Province    *BPSDetail `json:"province,omitempty"`
}

// BPSDetail captures the BPS code and display name.
type BPSDetail struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// RegionScore carries fuzzy-search scoring metadata when available.
type RegionScore struct {
	FTS         *float64 `json:"fts,omitempty"`
	Subdistrict *float64 `json:"subdistrict,omitempty"`
	District    *float64 `json:"district,omitempty"`
	City        *float64 `json:"city,omitempty"`
	Province    *float64 `json:"province,omitempty"`
}

// RegionWithScore binds the persisted region with optional scoring artefacts.
type RegionWithScore struct {
	Region Region
	Score  *RegionScore
}
