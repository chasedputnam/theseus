package tts

import (
	"encoding/json"
	"io"
	"os"
)

func writeFileImpl(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}

func removeFileImpl(path string) {
	os.Remove(path)
}

func decodeJSONImpl(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
