package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

type Signature struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	IsDefault bool  `json:"is_default"`
}

func (s *Server) registerSignaturesRoutes() {
	s.mux.HandleFunc("/api/signatures", s.withAuth(s.handleSignatures))
	s.mux.HandleFunc("/api/signatures/", s.withAuth(s.handleSignatureByID))
}

func (s *Server) signaturesFile(user string) string {
	return s.cfg.DataDir + "/signatures_" + sanitizeFilename(user) + ".json"
}

func (s *Server) loadSignatures(user string) []*Signature {
	var sigs []*Signature
	storage.ReadJSON(s.signaturesFile(user), &sigs)
	if sigs == nil {
		sigs = []*Signature{}
	}
	return sigs
}

func (s *Server) saveSignatures(user string, sigs []*Signature) error {
	return storage.WriteJSON(s.signaturesFile(user), sigs)
}

func (s *Server) handleSignatures(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.loadSignatures(user))
	case http.MethodPost:
		var sig Signature
		if err := json.NewDecoder(r.Body).Decode(&sig); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		sig.ID = uuid.New().String()
		sigs := s.loadSignatures(user)
		if sig.IsDefault {
			for _, s := range sigs {
				s.IsDefault = false
			}
		}
		sigs = append(sigs, &sig)
		s.saveSignatures(user, sigs)
		writeJSON(w, http.StatusOK, &sig)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSignatureByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := strings.TrimPrefix(r.URL.Path, "/api/signatures/")
	sigs := s.loadSignatures(user)

	switch r.Method {
	case http.MethodGet:
		for _, sig := range sigs {
			if sig.ID == id {
				writeJSON(w, http.StatusOK, sig)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodPut, http.MethodPatch:
		var req Signature
		json.NewDecoder(r.Body).Decode(&req)
		for i, sig := range sigs {
			if sig.ID == id {
				req.ID = id
				if req.IsDefault {
					for _, s := range sigs {
						s.IsDefault = false
					}
				}
				sigs[i] = &req
				s.saveSignatures(user, sigs)
				writeJSON(w, http.StatusOK, &req)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodDelete:
		for i, sig := range sigs {
			if sig.ID == id {
				sigs = append(sigs[:i], sigs[i+1:]...)
				s.saveSignatures(user, sigs)
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
