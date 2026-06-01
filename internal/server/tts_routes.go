package server

import (
	"io"
	"net/http"

	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/tts"
)

func (s *Server) registerTTSRoutes() {
	s.mux.HandleFunc("/api/tts/synthesize", s.withAuth(s.handleTTSSynthesize))
	s.mux.HandleFunc("/api/tts/stats", s.withAuth(s.handleTTSStats))
	s.mux.HandleFunc("/api/stt/transcribe", s.withAuth(s.handleSTTTranscribe))
	s.mux.HandleFunc("/api/stt/stats", s.withAuth(s.handleSTTStats))
}

func (s *Server) ttsService() *tts.Service {
	return tts.New(
		settings.GetString("tts_provider"),
		settings.GetString("tts_endpoint"),
		settings.GetString("tts_model"),
		settings.GetString("tts_voice"),
		settings.GetString("tts_speed"),
	)
}

func (s *Server) sttService() *tts.STTService {
	return tts.NewSTT(
		settings.GetString("stt_provider"),
		settings.GetString("stt_endpoint"),
		settings.GetString("stt_model"),
		settings.GetString("stt_language"),
	)
}

func (s *Server) handleTTSSynthesize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	svc := s.ttsService()
	if !svc.Available() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "TTS not available"})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := decodeBody(r, &req); err != nil || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "text required"})
		return
	}
	audio, contentType, err := svc.Synthesize(r.Context(), req.Text)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(audio)
}

func (s *Server) handleTTSStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ttsService().Stats())
}

func (s *Server) handleSTTTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	svc := s.sttService()
	if !svc.Available() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "STT not available"})
		return
	}
	r.ParseMultipartForm(50 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file"})
		return
	}
	defer file.Close()
	audio, err := io.ReadAll(file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	text, err := svc.Transcribe(r.Context(), audio, header.Filename)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) handleSTTStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sttService().Stats())
}
