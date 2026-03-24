package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dotjm/auth-proxy-poc/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

// CreateSession inserts a new session and returns the generated ID.
func (s *Store) CreateSession(ctx context.Context, sess *model.Session) error {
	credJSON, err := json.Marshal(sess.Credentials)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO sessions (name, target_host, auth_type, credentials, status, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at, updated_at`,
		sess.Name, sess.TargetHost, sess.AuthType, credJSON, sess.Status, sess.ExpiresAt,
	).Scan(&sess.ID, &sess.CreatedAt, &sess.UpdatedAt)
	return err
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(ctx context.Context, id string) (*model.Session, error) {
	sess := &model.Session{}
	var credJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, target_host, auth_type, credentials, status, expires_at, created_at, updated_at
		 FROM sessions WHERE id = $1`, id,
	).Scan(&sess.ID, &sess.Name, &sess.TargetHost, &sess.AuthType, &credJSON,
		&sess.Status, &sess.ExpiresAt, &sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(credJSON, &sess.Credentials); err != nil {
		return nil, fmt.Errorf("unmarshal credentials: %w", err)
	}
	return sess, nil
}

// ListSessions returns all sessions.
func (s *Store) ListSessions(ctx context.Context) ([]*model.Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, target_host, auth_type, credentials, status, expires_at, created_at, updated_at
		 FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*model.Session
	for rows.Next() {
		sess := &model.Session{}
		var credJSON []byte
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.TargetHost, &sess.AuthType, &credJSON,
			&sess.Status, &sess.ExpiresAt, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(credJSON, &sess.Credentials); err != nil {
			return nil, fmt.Errorf("unmarshal credentials: %w", err)
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

// UpdateSessionStatus changes the status of a session.
func (s *Store) UpdateSessionStatus(ctx context.Context, id string, status model.SessionStatus) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET status = $1, updated_at = NOW() WHERE id = $2`, status, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found: %s", id)
	}
	return nil
}

// UpdateSessionCredentials updates credentials for a session.
func (s *Store) UpdateSessionCredentials(ctx context.Context, id string, creds map[string]string) error {
	credJSON, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET credentials = $1, updated_at = NOW() WHERE id = $2`, credJSON, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found: %s", id)
	}
	return nil
}

// DeleteSession removes a session.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session not found: %s", id)
	}
	return nil
}

// --- Proxy Key operations ---

// CreateProxyKey creates a new proxy key linked to a session.
func (s *Store) CreateProxyKey(ctx context.Context, pk *model.ProxyKey) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO proxy_keys (key, session_id) VALUES ($1, $2) RETURNING created_at`,
		pk.Key, pk.SessionID,
	).Scan(&pk.CreatedAt)
}

// GetSessionByProxyKey looks up the active session for a proxy key.
func (s *Store) GetSessionByProxyKey(ctx context.Context, key string) (*model.Session, error) {
	sess := &model.Session{}
	var credJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT s.id, s.name, s.target_host, s.auth_type, s.credentials, s.status, s.expires_at, s.created_at, s.updated_at
		 FROM sessions s
		 JOIN proxy_keys pk ON pk.session_id = s.id
		 WHERE pk.key = $1 AND s.status = 'active'`, key,
	).Scan(&sess.ID, &sess.Name, &sess.TargetHost, &sess.AuthType, &credJSON,
		&sess.Status, &sess.ExpiresAt, &sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if sess.ExpiresAt != nil && sess.ExpiresAt.Before(time.Now()) {
		// Mark as expired
		_ = s.UpdateSessionStatus(ctx, sess.ID, model.SessionStatusExpired)
		return nil, fmt.Errorf("session expired")
	}
	if err := json.Unmarshal(credJSON, &sess.Credentials); err != nil {
		return nil, fmt.Errorf("unmarshal credentials: %w", err)
	}
	return sess, nil
}

// DeleteProxyKey removes a proxy key.
func (s *Store) DeleteProxyKey(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM proxy_keys WHERE key = $1`, key)
	return err
}
