package model

import "time"

// Session represents an active auth session managed by the system.
type Session struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	TargetHost  string            `json:"target_host"`
	AuthType    AuthType          `json:"auth_type"` // cookie, bearer, custom_header
	Credentials map[string]string `json:"credentials"`
	Status      SessionStatus     `json:"status"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type AuthType string

const (
	AuthTypeCookie       AuthType = "cookie"
	AuthTypeBearer       AuthType = "bearer"
	AuthTypeCustomHeader AuthType = "custom_header"
)

type SessionStatus string

const (
	SessionStatusActive   SessionStatus = "active"
	SessionStatusExpired  SessionStatus = "expired"
	SessionStatusRevoked  SessionStatus = "revoked"
)

// ProxyKey maps an API key to a session for proxy authentication.
type ProxyKey struct {
	Key       string    `json:"key"`
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
}
