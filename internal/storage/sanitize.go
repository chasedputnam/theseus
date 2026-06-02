package storage

// SanitizeFilename returns a safe filename component from an arbitrary string,
// keeping only alphanumeric characters, underscores, and hyphens.
func SanitizeFilename(s string) string {
	var out []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			out = append(out, c)
		}
	}
	return string(out)
}
