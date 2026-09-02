package middleware

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// CORSProvider holds the current CORS allow-list. It is mutable so that the
// settings page can update it at runtime without a restart.
type CORSProvider struct {
	mu       sync.RWMutex
	origins  []string
	allowAll bool
}

// NewCORSProvider parses a comma-separated list of exact origins (e.g.
// "https://app.example.com,https://admin.example.com") or "*" to allow any
// origin. An empty value disables CORS entirely.
func NewCORSProvider(allowedOrigins string) *CORSProvider {
	p := &CORSProvider{}
	p.SetOrigins(allowedOrigins)
	return p
}

// SetOrigins replaces the allow-list at runtime (called by the settings API).
func (p *CORSProvider) SetOrigins(allowedOrigins string) {
	var origins []string
	allowAll := false
	for _, o := range strings.Split(allowedOrigins, ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if o == "*" {
			allowAll = true
		} else {
			origins = append(origins, o)
		}
	}
	p.mu.Lock()
	p.origins = origins
	p.allowAll = allowAll
	p.mu.Unlock()
}

// Origins returns the current configured list (comma-separated, for the
// settings page to display).
func (p *CORSProvider) Origins() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.allowAll {
		return "*"
	}
	return strings.Join(p.origins, ",")
}

// Handler returns the CORS middleware. It allows browser clients from the
// configured origins to call the API, WebDAV and iCal endpoints.
func (p *CORSProvider) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		p.mu.RLock()
		allowed := origin != "" && (p.allowAll || contains(p.origins, origin))
		p.mu.RUnlock()
		if allowed {
			// Reflect the exact origin so credentialed requests work, even
			// when configured as "*".
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods",
				"GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS, "+
					"PROPFIND, PROPPATCH, MKCOL, MKCALENDAR, COPY, MOVE, LOCK, UNLOCK, REPORT")
			c.Header("Access-Control-Allow-Headers",
				"Authorization, Content-Type, Range, Depth, Destination, "+
					"Overwrite, If-Match, If-None-Match, Brief, Prefer")
			c.Header("Access-Control-Expose-Headers",
				"Content-Range, Accept-Ranges, Content-Length, ETag, Last-Modified")
			c.Header("Access-Control-Max-Age", "86400")
		}
		// Only short-circuit real CORS preflights (Origin +
		// Access-Control-Request-Method). Plain WebDAV OPTIONS requests
		// (litmus, curl) carry neither and must reach the handler so it can
		// reply with DAV/Allow headers.
		if c.Request.Method == http.MethodOptions &&
			origin != "" && c.GetHeader("Access-Control-Request-Method") != "" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
