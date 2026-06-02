package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/llm"
	"github.com/google/uuid"
)

func (s *Server) registerCompareRoutes() {
	s.mux.HandleFunc("/api/compare/start", s.withAuth(s.handleCompareStart))
	s.mux.HandleFunc("/api/compare/vote", s.withAuth(s.handleCompareVote))
	s.mux.HandleFunc("/api/compare/record", s.withAuth(s.handleCompareVote))
	s.mux.HandleFunc("/api/compare/history", s.withAuth(s.handleCompareHistory))
	s.mux.HandleFunc("/api/compare/", s.withAuth(s.handleCompareByID))
}

func (s *Server) handleCompareStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)

	var req struct {
		Prompt     string `json:"prompt"`
		ModelA     string `json:"model_a"`
		ModelB     string `json:"model_b"`
		EndpointA  string `json:"endpoint_a"`
		EndpointB  string `json:"endpoint_b"`
		IsBlind    bool   `json:"is_blind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	compID := uuid.New().String()
	sidA := uuid.New().String()
	sidB := uuid.New().String()

	// Blind mapping: randomize which model is shown as left/right
	blindMapping := map[string]string{"left": "a", "right": "b"}
	if req.IsBlind && rand.Intn(2) == 1 {
		blindMapping = map[string]string{"left": "b", "right": "a"}
	}
	blindJSON, _ := json.Marshal(blindMapping)

	// Create ephemeral sessions
	for _, pair := range []struct{ sid, model, endpoint string }{
		{sidA, req.ModelA, req.EndpointA},
		{sidB, req.ModelB, req.EndpointB},
	} {
		sess := &db.Session{
			ID:          pair.sid,
			Name:        fmt.Sprintf("[CMP] %s", pair.model),
			EndpointURL: pair.endpoint,
			Model:       pair.model,
			Owner:       sql.NullString{String: user, Valid: user != ""},
			Headers:     "{}",
		}
		if err := s.db.CreateSession(sess); err != nil {
			log.Printf("db: CreateSession: %v", err)
		}
	}

	// Create comparison record
	comp := &db.Comparison{
		ID:           compID,
		Owner:        sql.NullString{String: user, Valid: user != ""},
		Prompt:       req.Prompt,
		ModelA:       req.ModelA,
		ModelB:       req.ModelB,
		EndpointA:    req.EndpointA,
		EndpointB:    req.EndpointB,
		IsBlind:      req.IsBlind,
		BlindMapping: sql.NullString{String: string(blindJSON), Valid: true},
	}
	if err := s.db.CreateComparison(comp); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Stream both responses in parallel
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	var sseM sync.Mutex
	sendEvent := func(event, data string) {
		sseM.Lock()
		defer sseM.Unlock()
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	var wg sync.WaitGroup
	var responseA, responseB strings.Builder
	var muA, muB sync.Mutex

	streamModel := func(side, endpointURL, model string, sb *strings.Builder, mu *sync.Mutex) {
		defer wg.Done()
		client := llm.New()
		ch, err := client.Stream(r.Context(), llm.StreamRequest{
			URL:      endpointURL,
			Model:    model,
			Messages: []llm.Message{{Role: "user", Content: req.Prompt}},
		})
		if err != nil {
			d, _ := json.Marshal(map[string]string{"side": side, "error": err.Error()})
			sendEvent("error", string(d))
			return
		}
		for chunk := range ch {
			if chunk.Error != nil {
				break
			}
			if chunk.Delta != "" {
				mu.Lock()
				sb.WriteString(chunk.Delta)
				mu.Unlock()
				d, _ := json.Marshal(map[string]string{"side": side, "delta": chunk.Delta})
				sendEvent("delta", string(d))
			}
		}
		d, _ := json.Marshal(map[string]string{"side": side, "status": "done"})
		sendEvent("side_done", string(d))
	}

	wg.Add(2)
	go streamModel("a", req.EndpointA, req.ModelA, &responseA, &muA)
	go streamModel("b", req.EndpointB, req.ModelB, &responseB, &muB)
	wg.Wait()

	// Persist responses
	comp.ResponseA = sql.NullString{String: responseA.String(), Valid: true}
	comp.ResponseB = sql.NullString{String: responseB.String(), Valid: true}
	if err := s.db.UpdateComparison(comp); err != nil {
		log.Printf("db: UpdateComparison: %v", err)
	}

	d, _ := json.Marshal(map[string]string{"comparison_id": compID, "status": "done"})
	sendEvent("done", string(d))
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) handleCompareVote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ComparisonID string `json:"comparison_id"`
		Winner       string `json:"winner"` // "a", "b", or "tie"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	comp, err := s.db.GetComparison(req.ComparisonID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "comparison not found"})
		return
	}
	now := time.Now()
	comp.Winner = sql.NullString{String: req.Winner, Valid: true}
	comp.VotedAt = sql.NullTime{Time: now, Valid: true}
	if err := s.db.UpdateComparison(comp); err != nil {
		log.Printf("db: UpdateComparison: %v", err)
	}

	// Reveal model identities
	writeJSON(w, http.StatusOK, map[string]any{
		"winner":  req.Winner,
		"model_a": comp.ModelA,
		"model_b": comp.ModelB,
	})
}

func (s *Server) handleCompareHistory(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	comps, err := s.db.ListComparisons(user, 50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if comps == nil {
		comps = []*db.Comparison{}
	}
	writeJSON(w, http.StatusOK, comps)
}

func (s *Server) handleCompareByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/compare/")
	comp, err := s.db.GetComparison(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, comp)
}
