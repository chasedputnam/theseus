package tts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// STTProvider is the STT backend type.
type STTProvider string

const (
	STTProviderDisabled STTProvider = "disabled"
	STTProviderBrowser  STTProvider = "browser"
	STTProviderWhisper  STTProvider = "whisper"
	STTProviderAPI      STTProvider = "api"
)

// STTService handles speech-to-text transcription.
type STTService struct {
	provider   STTProvider
	endpoint   string
	model      string
	language   string
	httpClient *http.Client
}

// NewSTT creates an STT service from settings.
func NewSTT(provider, endpoint, model, language string) *STTService {
	return &STTService{
		provider:   STTProvider(provider),
		endpoint:   endpoint,
		model:      model,
		language:   language,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// Available returns true if STT is configured and usable server-side.
func (s *STTService) Available() bool {
	return s.provider != STTProviderDisabled && s.provider != STTProviderBrowser
}

// Transcribe converts audio bytes to text.
func (s *STTService) Transcribe(ctx context.Context, audio []byte, filename string) (string, error) {
	switch s.provider {
	case STTProviderWhisper:
		return s.transcribeWhisper(ctx, audio, filename)
	case STTProviderAPI:
		return s.transcribeAPI(ctx, audio, filename)
	default:
		return "", fmt.Errorf("STT provider %q not available server-side", s.provider)
	}
}

func (s *STTService) transcribeWhisper(ctx context.Context, audio []byte, filename string) (string, error) {
	// Use local whisper CLI
	model := s.model
	if model == "" {
		model = "base"
	}
	// Write audio to temp file
	tmpFile := "/tmp/theseus_stt_" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := writeFile(tmpFile, audio); err != nil {
		return "", err
	}
	defer removeFile(tmpFile)

	args := []string{tmpFile, "--model", model, "--output_format", "txt"}
	if s.language != "" {
		args = append(args, "--language", s.language)
	}
	cmd := exec.CommandContext(ctx, "whisper", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("whisper: %w: %s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *STTService) transcribeAPI(ctx context.Context, audio []byte, filename string) (string, error) {
	endpoint := s.endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/audio/transcriptions"
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	fw.Write(audio)
	mw.WriteField("model", s.model)
	if s.language != "" {
		mw.WriteField("language", s.language)
	}
	mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("STT API error %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := decodeJSON(resp.Body, &result); err != nil {
		return "", err
	}
	return result.Text, nil
}

// Stats returns service status info.
func (s *STTService) Stats() map[string]any {
	return map[string]any{
		"provider":  string(s.provider),
		"available": s.Available(),
		"model":     s.model,
	}
}

// helpers to avoid importing os/encoding/json directly
func writeFile(path string, data []byte) error {
	import_os := func() error {
		return nil
	}
	_ = import_os
	return writeFileImpl(path, data)
}

func removeFile(path string) {
	removeFileImpl(path)
}

func decodeJSON(r io.Reader, v any) error {
	return decodeJSONImpl(r, v)
}
