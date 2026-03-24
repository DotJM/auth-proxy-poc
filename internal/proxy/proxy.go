package proxy

import (
	"log"
	"net/http"
	"strings"

	"github.com/dotjm/auth-proxy-poc/internal/model"
	"github.com/dotjm/auth-proxy-poc/internal/store"
	"github.com/elazarl/goproxy"
)

const (
	// ProxyAuthHeader is the header clients send to identify their session.
	// This header is stripped before forwarding to the target.
	ProxyAuthHeader = "X-Auth-Proxy-Key"
)

// NewProxyServer creates a goproxy server that injects credentials from the store.
func NewProxyServer(s *store.Store) *goproxy.ProxyHttpServer {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false

	// Handle HTTP requests
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		return handleRequest(req, ctx, s)
	})

	// Handle HTTPS CONNECT — allow all, then modify the tunneled request
	proxy.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		// For HTTPS, we need to MITM to inject headers.
		// The client must trust our CA certificate.
		return goproxy.MitmConnect, host
	})

	return proxy
}

func handleRequest(req *http.Request, ctx *goproxy.ProxyCtx, s *store.Store) (*http.Request, *http.Response) {
	proxyKey := req.Header.Get(ProxyAuthHeader)
	if proxyKey == "" {
		log.Printf("[proxy] rejected: missing %s header from %s", ProxyAuthHeader, req.RemoteAddr)
		return req, goproxy.NewResponse(req, goproxy.ContentTypeText,
			http.StatusProxyAuthRequired, "missing X-Auth-Proxy-Key header")
	}

	// Always strip the proxy auth header before forwarding
	req.Header.Del(ProxyAuthHeader)

	// Look up session by proxy key
	sess, err := s.GetSessionByProxyKey(req.Context(), proxyKey)
	if err != nil {
		log.Printf("[proxy] rejected: invalid or expired key from %s: %v", req.RemoteAddr, err)
		return req, goproxy.NewResponse(req, goproxy.ContentTypeText,
			http.StatusForbidden, "invalid or expired proxy key")
	}

	// Verify the request target matches the session's target host
	reqHost := req.URL.Hostname()
	if !hostMatches(reqHost, sess.TargetHost) {
		log.Printf("[proxy] rejected: host mismatch %s != %s", reqHost, sess.TargetHost)
		return req, goproxy.NewResponse(req, goproxy.ContentTypeText,
			http.StatusForbidden, "target host not allowed for this session")
	}

	// Inject credentials based on auth type
	injectCredentials(req, sess)

	log.Printf("[proxy] forwarding %s %s (session=%s)", req.Method, req.URL.String(), sess.ID)
	return req, nil
}

func injectCredentials(req *http.Request, sess *model.Session) {
	switch sess.AuthType {
	case model.AuthTypeCookie:
		// Merge cookies from session credentials
		existing := req.Header.Get("Cookie")
		var parts []string
		if existing != "" {
			parts = append(parts, existing)
		}
		for name, value := range sess.Credentials {
			parts = append(parts, name+"="+value)
		}
		if len(parts) > 0 {
			req.Header.Set("Cookie", strings.Join(parts, "; "))
		}

	case model.AuthTypeBearer:
		token, ok := sess.Credentials["token"]
		if ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}

	case model.AuthTypeCustomHeader:
		for headerName, headerValue := range sess.Credentials {
			req.Header.Set(headerName, headerValue)
		}
	}
}

// hostMatches checks if the request host matches the session target.
// Supports wildcard prefix like *.example.com
func hostMatches(reqHost, targetHost string) bool {
	if reqHost == targetHost {
		return true
	}
	if strings.HasPrefix(targetHost, "*.") {
		suffix := targetHost[1:] // .example.com
		return strings.HasSuffix(reqHost, suffix)
	}
	return false
}
