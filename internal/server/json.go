package server

import (
	"encoding/json"
	"net/http"
)

func encodeJSONImpl(w http.ResponseWriter, v any) {
	json.NewEncoder(w).Encode(v)
}
