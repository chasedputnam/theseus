package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

type Contact struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Emails  []string `json:"emails"`
	Phones  []string `json:"phones"`
	Company string   `json:"company"`
	Notes   string   `json:"notes"`
}

func (s *Server) registerContactsRoutes() {
	s.mux.HandleFunc("/api/contacts", s.withAuth(s.handleContacts))
	s.mux.HandleFunc("/api/contacts/search", s.withAuth(s.handleContactsSearch))
	s.mux.HandleFunc("/api/contacts/export", s.withAuth(s.handleContactsExport))
	s.mux.HandleFunc("/api/contacts/import", s.withAuth(s.handleContactsImport))
	s.mux.HandleFunc("/api/contacts/clear", s.withAuth(s.handleContactsClear))
	s.mux.HandleFunc("/api/contacts/config", s.withAuth(s.handleContactsConfig))
	s.mux.HandleFunc("/api/contacts/list", s.withAuth(s.handleContactsList))
	s.mux.HandleFunc("/api/contacts/add", s.withAuth(s.handleContactsAdd))
	s.mux.HandleFunc("/api/contacts/", s.withAuth(s.handleContactByID))
}

func (s *Server) contactsFile() string {
	return filepath.Join(s.cfg.DataDir, "contacts.json")
}

func (s *Server) loadContacts() ([]*Contact, error) {
	var contacts []*Contact
	if err := storage.ReadJSON(s.contactsFile(), &contacts); err != nil {
		if os.IsNotExist(err) {
			return []*Contact{}, nil
		}
		return nil, err
	}
	return contacts, nil
}

func (s *Server) saveContacts(contacts []*Contact) error {
	return storage.WriteJSON(s.contactsFile(), contacts)
}

func (s *Server) handleContacts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		contacts, err := s.loadContacts()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, contacts)
	case http.MethodPost:
		var req Contact
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		req.ID = uuid.New().String()
		contacts, _ := s.loadContacts()
		contacts = append(contacts, &req)
		s.saveContacts(contacts)
		writeJSON(w, http.StatusOK, &req)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleContactsSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	contacts, err := s.loadContacts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if q == "" {
		writeJSON(w, http.StatusOK, contacts)
		return
	}
	var results []*Contact
	for _, c := range contacts {
		if strings.Contains(strings.ToLower(c.Name), q) ||
			strings.Contains(strings.ToLower(c.Company), q) {
			results = append(results, c)
			continue
		}
		for _, e := range c.Emails {
			if strings.Contains(strings.ToLower(e), q) {
				results = append(results, c)
				break
			}
		}
	}
	if results == nil {
		results = []*Contact{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleContactByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/contacts/")
	contacts, _ := s.loadContacts()

	switch r.Method {
	case http.MethodGet:
		for _, c := range contacts {
			if c.ID == id {
				writeJSON(w, http.StatusOK, c)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodPut, http.MethodPatch:
		var req Contact
		json.NewDecoder(r.Body).Decode(&req)
		for i, c := range contacts {
			if c.ID == id {
				req.ID = id
				contacts[i] = &req
				s.saveContacts(contacts)
				writeJSON(w, http.StatusOK, &req)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodDelete:
		for i, c := range contacts {
			if c.ID == id {
				contacts = append(contacts[:i], contacts[i+1:]...)
				s.saveContacts(contacts)
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleContactsExport(w http.ResponseWriter, r *http.Request) {
	contacts, err := s.loadContacts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="contacts.json"`)
	encodeJSON(w, contacts)
}

func (s *Server) handleContactsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var incoming []*Contact
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	existing, _ := s.loadContacts()
	for _, c := range incoming {
		if c.ID == "" {
			c.ID = uuid.New().String()
		}
		existing = append(existing, c)
	}
	s.saveContacts(existing)
	writeJSON(w, http.StatusOK, map[string]int{"imported": len(incoming)})
}

func (s *Server) handleContactsClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.saveContacts([]*Contact{})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleContactsConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"carddav_url":      settings.GetString("carddav_url"),
			"carddav_username": settings.GetString("carddav_username"),
			"configured":       settings.GetString("carddav_url") != "",
		})
	case http.MethodPost, http.MethodPut:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		current := settings.Load()
		for k, v := range req {
			current[k] = v
		}
		if err := settings.Save(current); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleContactsList(w http.ResponseWriter, r *http.Request) {
	contacts, err := s.loadContacts()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contacts, "count": len(contacts)})
}

func (s *Server) handleContactsAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req Contact
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	req.ID = uuid.New().String()
	contacts, _ := s.loadContacts()
	contacts = append(contacts, &req)
	s.saveContacts(contacts)
	writeJSON(w, http.StatusOK, &req)
}


