package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Provider is the TTS backend type.
type Provider string

const (
	ProviderDisabled Provider = "disabled"
	ProviderBrowser  Provider = "browser"
	ProviderOpenAI   Provider = "openai"
	ProviderKokoro   Provider = "kokoro"
)

// Service handles text-to-speech synthesis.
type Service struct {
	provider   Provider
	endpoint   string
	model      string
	voice      string
	speed      string
	httpClient *http.Client
}

// New creates a TTS Service from settings.
func New(provider, endpoint, model, voice, speed string) *Service {
	return &Service{
		provider:   Provider(provider),
		endpoint:   endpoint,
		model:      model,
		voice:      voice,
		speed:      speed,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Available returns true if TTS is configured and usable server-side.
func (s *Service) Available() bool {
	return s.provider != ProviderDisabled && s.provider != ProviderBrowser
}

// Synthesize converts text to audio bytes (MP3/WAV).
func (s *Service) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	switch s.provider {
	case ProviderOpenAI:
		return s.synthesizeOpenAI(ctx, text)
	case ProviderKokoro:
		return s.synthesizeKokoro(ctx, text)
	default:
		return nil, "", fmt.Errorf("TTS provider %q not available server-side", s.provider)
	}
}

func (s *Service) synthesizeOpenAI(ctx context.Context, text string) ([]byte, string, error) {
	endpoint := s.endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/audio/speech"
	}
	model := s.model
	if model == "" {
		model = "tts-1"
	}
	voice := s.voice
	if voice == "" {
		voice = "alloy"
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"input": text,
		"voice": voice,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("TTS error %d: %s", resp.StatusCode, string(b))
	}
	audio, err := io.ReadAll(resp.Body)
	return audio, "audio/mpeg", err
}

func (s *Service) synthesizeKokoro(ctx context.Context, text string) ([]byte, string, error) {
	endpoint := s.endpoint
	if endpoint == "" {
		endpoint = "http://localhost:8880/v1/audio/speech"
	}
	return s.synthesizeOpenAICompat(ctx, text, endpoint)
}

func (s *Service) synthesizeOpenAICompat(ctx context.Context, text, endpoint string) ([]byte, string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": s.model,
		"input": text,
		"voice": s.voice,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	audio, err := io.ReadAll(resp.Body)
	return audio, "audio/wav", err
}

// Stats returns service status info.
func (s *Service) Stats() map[string]any {
	return map[string]any{
		"provider":  string(s.provider),
		"available": s.Available(),
		"model":     s.model,
		"voice":     s.voice,
	}
}

// ClearCache is a no-op placeholder; TTS caching is not yet implemented.
func (s *Service) ClearCache() {}
