package storage

import "crypto/rand"

// RandBytes fills b with cryptographically random bytes.
func RandBytes(b []byte) {
	if _, err := rand.Read(b); err != nil {
		panic("storage: rand.Read failed: " + err.Error())
	}
}
