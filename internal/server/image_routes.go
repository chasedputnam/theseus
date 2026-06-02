package server

import (
	"io"
	"net/http"
	"strings"

	"github.com/chaseputnam/theseus/internal/settings"
)

func (s *Server) registerImageRoutes() {
	s.mux.HandleFunc("/api/image/inpaint", s.withAuth(s.handleImageInpaint))
	s.mux.HandleFunc("/api/image/upscale", s.withAuth(s.handleImageUpscale))
	s.mux.HandleFunc("/api/image/upscale-local", s.withAuth(s.handleImageProxy("upscale")))
	s.mux.HandleFunc("/api/image/harmonize", s.withAuth(s.handleImageProxy("harmonize")))
	s.mux.HandleFunc("/api/image/sharpen", s.withAuth(s.handleImageProxy("sharpen")))
	s.mux.HandleFunc("/api/image/remove-bg", s.withAuth(s.handleImageProxy("remove-bg")))
	s.mux.HandleFunc("/api/image/remove-background", s.withAuth(s.handleImageRemoveBackground))
	s.mux.HandleFunc("/api/image/", s.withAuth(s.handleImageOps))
}

func (s *Server) handleImageProxy(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		backendURL := settings.GetString("diffusion_backend_url")
		if backendURL == "" {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no diffusion backend configured"})
			return
		}
		target := strings.TrimRight(backendURL, "/") + "/" + op
		proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "" {
			proxyReq.Header.Set("Content-Type", ct)
		}
		resp, err := http.DefaultClient.Do(proxyReq)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

func (s *Server) handleImageInpaint(w http.ResponseWriter, r *http.Request) {
	s.handleImageProxy("inpaint")(w, r)
}

func (s *Server) handleImageUpscale(w http.ResponseWriter, r *http.Request) {
	s.handleImageProxy("upscale")(w, r)
}

func (s *Server) handleImageRemoveBackground(w http.ResponseWriter, r *http.Request) {
	s.handleImageProxy("remove-background")(w, r)
}

func (s *Server) handleImageOps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}
