package model

import "github.com/ilmimris/wilayah-indonesia/internal/entity"

// RegionResponse is the HTTP representation of a region record.
type RegionResponse struct {
	ID          string              `json:"id"`
	Subdistrict string              `json:"subdistrict"`
	District    string              `json:"district"`
	City        string              `json:"city"`
	Province    string              `json:"province"`
	PostalCode  string              `json:"postal_code"`
	FullText    string              `json:"full_text"`
	BPS         *entity.RegionBPS   `json:"bps,omitempty"`
	Scores      *entity.RegionScore `json:"scores,omitempty"`
}

// SearchResponse wraps a list of region responses for transport layers.
type SearchResponse struct {
	Items []RegionResponse `json:"items"`
}
