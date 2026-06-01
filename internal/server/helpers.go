package server

import (
	"encoding/json"
	"net/http"
)

func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
