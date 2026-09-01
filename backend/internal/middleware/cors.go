package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS allows browser clients from the configured origins to call the API,
// WebDAV and iCal endpoints. `allowedOrigins` is a comma-separated list of
// exact origins (e.g. "https://app.example.com,https://admin.example.com")
// or "*" to allow any origin.
func CORS(allowedOrigins string) gin.HandlerFunc {
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

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowed := origin != "" && (allowAll || contains(origins, origin))
		if allowed {
			// Reflect the exact origin so credentialed requests work, even
			// when configured as "*".
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods",
				"GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS, "+
					"PROPFIND, PROPPATCH, MKCOL, COPY, MOVE, LOCK, UNLOCK, REPORT")
			c.Header("Access-Control-Allow-Headers",
				"Authorization, Content-Type, Range, Depth, Destination, "+
					"Overwrite, If-Match, If-None-Match, Brief, Prefer")
			c.Header("Access-Control-Expose-Headers",
				"Content-Range, Accept-Ranges, Content-Length, ETag, Last-Modified")
			c.Header("Access-Control-Max-Age", "86400")
		}
		if c.Request.Method == http.MethodOptions {
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
