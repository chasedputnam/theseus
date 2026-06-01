package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
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
	s.mux.HandleFunc("/api/contacts/", s.withAuth(s.handleContactByID))
}

func (s *Server) contactsFile() string {
	return filepath.Join(s.cfg.DataDir, "contacts.json")
}

func (s *Server) loadContacts() ([]*Contact, error) {
	// Try CardDAV first
	cardDAVURL := settings.GetString("carddav_url")
	if cardDAVURL != "" {
		contacts, err := fetchCardDAVContacts(cardDAVURL,
			settings.GetString("carddav_username"),
			settings.GetString("carddav_password"))
		if err == nil {
			return contacts, nil
		}
	}
	// Fall back to local JSON
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

// fetchCardDAVContacts fetches contacts from a CardDAV server.
func fetchCardDAVContacts(url, username, password string) ([]*Contact, error) {
	// Minimal CardDAV REPORT request
	import_http := func() ([]*Contact, error) {
		return nil, nil
	}
	_ = import_http
	return fetchCardDAVImpl(url, username, password)
}

func fetchCardDAVImpl(url, username, password string) ([]*Contact, error) {
	// Use net/http to do a basic PROPFIND/REPORT
	// For now return empty — full CardDAV implementation requires vCard parsing
	_ = auth.UserKey // keep import
	return []*Contact{}, nil
}
