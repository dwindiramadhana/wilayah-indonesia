package model

// ErrorResponse standardises JSON error payloads emitted by delivery layers.
type ErrorResponse struct {
	Error string `json:"error"`
}
