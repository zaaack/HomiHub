package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/models"
)

const (
	RoleParent = "parent"
	RoleChild  = "child"
)

type Claims struct {
	UserID   string `json:"sub"`
	TeamID string `json:"fam"`
	Role     string `json:"role"`
	Kind     string `json:"kind"`
	jwt.RegisteredClaims
}

const (
	CtxClaims = "claims"
	CtxDB     = "db"
)

func ClaimsOf(c *gin.Context) *Claims {
	cl, _ := c.Get(CtxClaims)
	return cl.(*Claims)
}

// DB returns a fresh tenant-scoped database handle for the current request.
func DB(c *gin.Context) *gorm.DB {
	cl := ClaimsOf(c)
	base := c.MustGet(CtxDB).(*gorm.DB)
	return base.Session(&gorm.Session{NewDB: true}).Where("team_id = ?", cl.TeamID)
}

func ScopedDB(base *gorm.DB, teamID string) *gorm.DB {
	return base.Session(&gorm.Session{NewDB: true}).Where("team_id = ?", teamID)
}

func Auth(base *gorm.DB, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			if claims := ResolveClaims(base, secret, token); claims != nil {
				c.Set(CtxClaims, claims)
				c.Set(CtxDB, base)
				c.Next()
				return
			}
		}
		httpx.UnauthorizedT(c, "unauthorized")
		c.Abort()
	}
}

// RequireRole re-reads the role from the DB (fresh per request) and requires
// it be one of the given roles.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		cl := ClaimsOf(c)
		if cl.Kind != "user" {
			httpx.ForbiddenT(c, "forbidden")
			c.Abort()
			return
		}
		var u models.User
		if err := DB(c).First(&u, "id = ?", cl.UserID).Error; err != nil {
			httpx.UnauthorizedT(c, "unauthorized")
			c.Abort()
			return
		}
		for _, r := range roles {
			if u.Role == r {
				c.Next()
				return
			}
		}
		httpx.ForbiddenT(c, "forbidden")
		c.Abort()
	}
}

func RequireWriteRole() gin.HandlerFunc {
	return RequireRole(RoleParent, RoleChild)
}

func RequireParent() gin.HandlerFunc {
	return RequireRole(RoleParent)
}

func SignToken(secret string, claims *Claims, ttl time.Duration) (string, error) {
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(ttl))
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(secret))
}
