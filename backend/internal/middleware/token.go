package middleware

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/models"
)

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func RandomToken(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ResolveClaims resolves a Bearer token into Claims. Primary mechanism is a
// stateful token row in the tokens table; legacy JWT is a fallback.
func ResolveClaims(base *gorm.DB, secret, raw string) *Claims {
	if claims, ok := resolveFromDB(base, raw); ok {
		return claims
	}
	return resolveJWT(secret, raw)
}

func resolveFromDB(base *gorm.DB, raw string) (*Claims, bool) {
	var tok models.Token
	if err := base.Where("token_hash = ?", HashToken(raw)).First(&tok).Error; err != nil {
		return nil, false
	}
	if tok.RevokedAt != nil {
		return nil, false
	}
	if tok.ExpiresAt != nil && time.Now().After(*tok.ExpiresAt) {
		return nil, false
	}
	if tok.Kind == "user" {
		var u models.User
		if err := base.First(&u, "id = ?", tok.SubjectID).Error; err != nil {
			return nil, false
		}
		var uf models.TeamMember
		role := u.Role
		if err := base.Where("user_id = ? AND team_id = ?", u.ID, tok.TeamID).First(&uf).Error; err == nil {
			role = uf.Role
		}
		now := time.Now()
		base.Model(&tok).Update("last_used_at", &now)
		return &Claims{UserID: u.ID, TeamID: tok.TeamID, Role: role, Kind: "user"}, true
	}
	return nil, false
}

func resolveJWT(secret, raw string) *Claims {
	cl := &Claims{}
	t, err := jwt.ParseWithClaims(raw, cl, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil || !t.Valid {
		return nil
	}
	return cl
}

// CreateToken issues a stateful session token and returns the raw token.
func CreateToken(db *gorm.DB, teamID, kind, subjectID, name, ip, userAgent string, ttl time.Duration) (string, error) {
	raw := RandomToken(32)
	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}
	tok := models.Token{
		ID:        uuid.Must(uuid.NewV7()).String(),
		TeamID:  teamID,
		Kind:      kind,
		SubjectID: subjectID,
		Name:      name,
		TokenHash: HashToken(raw),
		IP:        ip,
		UserAgent: userAgent,
		ExpiresAt: expiresAt,
	}
	if err := db.Create(&tok).Error; err != nil {
		return "", err
	}
	return raw, nil
}
