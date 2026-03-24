package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"

	"github.com/dotjm/auth-proxy-poc/internal/model"
	"github.com/dotjm/auth-proxy-poc/internal/store"
)

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

// RegisterRoutes sets up the API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/sessions", h.createSession)
	mux.HandleFunc("GET /api/sessions", h.listSessions)
	mux.HandleFunc("GET /api/sessions/{id}", h.getSession)
	mux.HandleFunc("PATCH /api/sessions/{id}", h.updateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", h.deleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/credentials", h.updateCredentials)
	mux.HandleFunc("POST /api/sessions/{id}/revoke", h.revokeSession)
}

type createSessionRequest struct {
	Name       string            `json:"name"`
	TargetHost string            `json:"target_host"`
	AuthType   model.AuthType    `json:"auth_type"`
	Credentials map[string]string `json:"credentials,omitempty"`
}

type createSessionResponse struct {
	Session  *model.Session `json:"session"`
	ProxyKey string         `json:"proxy_key"`
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.TargetHost == "" || req.AuthType == "" {
		writeError(w, http.StatusBadRequest, "name, target_host, and auth_type are required")
		return
	}
	if req.AuthType != model.AuthTypeCookie && req.AuthType != model.AuthTypeBearer && req.AuthType != model.AuthTypeCustomHeader {
		writeError(w, http.StatusBadRequest, "auth_type must be cookie, bearer, or custom_header")
		return
	}

	sess := &model.Session{
		Name:        req.Name,
		TargetHost:  req.TargetHost,
		AuthType:    req.AuthType,
		Credentials: req.Credentials,
		Status:      model.SessionStatusActive,
	}
	if sess.Credentials == nil {
		sess.Credentials = make(map[string]string)
	}

	if err := h.store.CreateSession(r.Context(), sess); err != nil {
		log.Printf("create session error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// Generate proxy key
	proxyKey := generateKey()
	pk := &model.ProxyKey{Key: proxyKey, SessionID: sess.ID}
	if err := h.store.CreateProxyKey(r.Context(), pk); err != nil {
		log.Printf("create proxy key error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to create proxy key")
		return
	}

	writeJSON(w, http.StatusCreated, createSessionResponse{Session: sess, ProxyKey: proxyKey})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.store.ListSessions(r.Context())
	if err != nil {
		log.Printf("list sessions error: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	if sessions == nil {
		sessions = []*model.Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := h.store.GetSession(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

type updateSessionRequest struct {
	Name       *string            `json:"name,omitempty"`
	TargetHost *string            `json:"target_host,omitempty"`
	AuthType   *model.AuthType    `json:"auth_type,omitempty"`
}

func (h *Handler) updateSession(w http.ResponseWriter, r *http.Request) {
	// Simple: only status updates for now. Full PATCH can be extended.
	writeError(w, http.StatusNotImplemented, "use specific endpoints like /revoke or /credentials")
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.DeleteSession(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateCredentialsRequest struct {
	Credentials map[string]string `json:"credentials"`
}

func (h *Handler) updateCredentials(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateCredentialsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.store.UpdateSessionCredentials(r.Context(), id, req.Credentials); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.UpdateSessionStatus(r.Context(), id, model.SessionStatusRevoked); err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// --- Helpers ---

func generateKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "apk_" + hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
