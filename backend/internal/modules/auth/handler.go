package moduleauth

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
)

const sessionTTL = 30 * 24 * time.Hour

type Handler struct {
	app *modules.App
}

func (h *Handler) ID() string { return "auth" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	return nil
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.POST("/auth/register", h.register)
	g.POST("/auth/login", h.login)
	g.GET("/auth/me", auth, h.me)
	g.GET("/auth/families", auth, h.listFamilies)
	g.POST("/auth/switch-team", auth, h.switchTeam)
	g.PATCH("/auth/profile", auth, h.updateProfile)
	g.POST("/auth/logout", auth, h.logout)
	g.GET("/auth/tokens", auth, h.listTokens)
	g.DELETE("/auth/tokens/:id", auth, h.revokeToken)
	g.POST("/auth/app-passwords", auth, h.createAppPassword)
	g.GET("/auth/app-passwords", auth, h.listAppPasswords)
	g.DELETE("/auth/app-passwords/:id", auth, h.revokeAppPassword)
}

type loginResp struct {
	Token  string        `json:"token"`
	User   models.User   `json:"user"`
		Team  models.Team `json:"team"`
}

// LoginResp is the shared login/join/switch response shape, exported for
// other modules (family join) to reuse.
type LoginResp = loginResp

func passwordStrong(pw string) bool {
	if len(pw) < 8 {
		return false
	}
	hasLetter, hasDigit := false, false
	for _, r := range pw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

type registerInput struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	TeamName string `json:"teamName"`
}

func (h *Handler) register(c *gin.Context) {
	var in registerInput
	if !httpx.Bind(c, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	in.TeamName = strings.TrimSpace(in.TeamName)
	if in.Name == "" || in.Email == "" {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	if !passwordStrong(in.Password) {
		httpx.BadRequestT(c, "weak_password")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	famName := in.TeamName
	if famName == "" {
		famName = in.Name + " 的空间"
	}
	err = h.app.DB.Transaction(func(tx *gorm.DB) error {
		fam := models.Team{
			ID:            uuid.Must(uuid.NewV7()).String(),
			Name:          famName,
			CalendarToken: middleware.RandomToken(16),
		}
		user := models.User{
			ID:           uuid.Must(uuid.NewV7()).String(),
			TeamID:     fam.ID,
			Email:        in.Email,
			PasswordHash: string(hash),
			Name:         in.Name,
			Role:         middleware.RoleParent,
		}
		fam.OwnerID = user.ID
		if err := tx.Create(&fam).Error; err != nil {
			return err
		}
		if err := tx.Create(&user).Error; err != nil {
			return err
		}
		return tx.Create(&models.TeamMember{UserID: user.ID, TeamID: fam.ID, Role: middleware.RoleParent}).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			httpx.BadRequestT(c, "email_registered")
			return
		}
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	h.respond(c, in.Email, in.Password)
}

type loginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(c *gin.Context) {
	var in loginInput
	if !httpx.Bind(c, &in) {
		return
	}
	h.respond(c, strings.ToLower(strings.TrimSpace(in.Email)), in.Password)
}

func (h *Handler) respond(c *gin.Context, email, password string) {
	var user models.User
	if err := h.app.DB.Where("email = ?", email).First(&user).Error; err != nil {
		httpx.UnauthorizedT(c, "email_or_password_wrong")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		httpx.UnauthorizedT(c, "email_or_password_wrong")
		return
	}
	var fam models.Team
	if err := h.app.DB.First(&fam, "id = ?", user.TeamID).Error; err != nil {
		httpx.UnauthorizedT(c, "unauthorized")
		return
	}
	var uf models.TeamMember
	if err := h.app.DB.Where("user_id = ? AND team_id = ?", user.ID, fam.ID).First(&uf).Error; err == nil {
		user.Role = uf.Role
	}
	token, err := middleware.CreateToken(h.app.DB, fam.ID, "user", user.ID, "登录会话", c.ClientIP(), c.Request.UserAgent(), sessionTTL)
	if err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	httpx.OK(c, loginResp{Token: token, User: user, Team: fam})
}

func (h *Handler) me(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var user models.User
	if err := middleware.DB(c).First(&user, "id = ?", cl.UserID).Error; err != nil {
		httpx.UnauthorizedT(c, "unauthorized")
		return
	}
	var fam models.Team
	h.app.DB.First(&fam, "id = ?", cl.TeamID)
	httpx.OK(c, gin.H{"user": user, "team": fam})
}

type teamEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	IsHome bool   `json:"isHome"`
}

func (h *Handler) listFamilies(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var rows []models.TeamMember
	if err := h.app.DB.Where("user_id = ?", cl.UserID).Find(&rows).Error; err != nil {
		httpx.ErrT(c, 500, "query_failed")
		return
	}
	out := make([]teamEntry, 0, len(rows))
	for _, r := range rows {
		var fam models.Team
		if err := h.app.DB.First(&fam, "id = ?", r.TeamID).Error; err != nil {
			continue
		}
		out = append(out, teamEntry{ID: fam.ID, Name: fam.Name, Role: r.Role, IsHome: r.TeamID == cl.TeamID})
	}
	httpx.OK(c, out)
}

func (h *Handler) switchTeam(c *gin.Context) {
	var in struct {
		TeamID string `json:"teamId"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	cl := middleware.ClaimsOf(c)
	var uf models.TeamMember
	if err := h.app.DB.Where("user_id = ? AND team_id = ?", cl.UserID, in.TeamID).First(&uf).Error; err != nil {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	var user models.User
	if err := h.app.DB.First(&user, "id = ?", cl.UserID).Error; err != nil {
		httpx.UnauthorizedT(c, "unauthorized")
		return
	}
	user.TeamID = uf.TeamID
	user.Role = uf.Role
	if err := h.app.DB.Save(&user).Error; err != nil {
		httpx.ErrT(c, 500, "save_failed")
		return
	}
	var fam models.Team
	h.app.DB.First(&fam, "id = ?", uf.TeamID)
	token, err := middleware.CreateToken(h.app.DB, fam.ID, "user", user.ID, "登录会话", c.ClientIP(), c.Request.UserAgent(), sessionTTL)
	if err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	httpx.OK(c, loginResp{Token: token, User: user, Team: fam})
}

func (h *Handler) updateProfile(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var in struct {
		Name string `json:"name"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	if err := middleware.DB(c).Model(&models.User{}).Where("id = ?", cl.UserID).Update("name", in.Name).Error; err != nil {
		httpx.ErrT(c, 500, "save_failed")
		return
	}
	var user models.User
	middleware.DB(c).First(&user, "id = ?", cl.UserID)
	httpx.OK(c, user)
}

func (h *Handler) logout(c *gin.Context) {
	raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	h.app.DB.Model(&models.Token{}).
		Where("token_hash = ?", middleware.HashToken(raw)).
		Update("revoked_at", time.Now())
	httpx.OK(c, gin.H{"ok": true})
}

// --- Token management ---

type tokenEntry struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name"`
	Token      string     `json:"token,omitempty"` // app passwords only
	IP         string     `json:"ip"`
	UserAgent  string     `json:"userAgent"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func (h *Handler) listTokens(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var tokens []models.Token
	h.app.DB.Where("subject_id = ? AND kind = ? AND revoked_at IS NULL", cl.UserID, "user").
		Order("created_at DESC").Find(&tokens)
	out := make([]tokenEntry, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenEntry{
			ID: t.ID, Kind: t.Kind, Name: t.Name, IP: t.IP,
			UserAgent: t.UserAgent, ExpiresAt: t.ExpiresAt,
			LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt,
		})
	}
	httpx.OK(c, out)
}

func (h *Handler) revokeToken(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	tokenID := c.Param("id")
	var tok models.Token
	if err := h.app.DB.Where("id = ? AND subject_id = ? AND revoked_at IS NULL", tokenID, cl.UserID).First(&tok).Error; err != nil {
		httpx.ErrT(c, 404, "not_found")
		return
	}
	if tok.TokenHash == middleware.HashToken(raw) {
		httpx.ErrT(c, 400, "cannot_revoke_current_token")
		return
	}
	now := time.Now()
	h.app.DB.Model(&tok).Update("revoked_at", now)
	httpx.OK(c, gin.H{"ok": true})
}

// --- App Passwords ---

func (h *Handler) createAppPassword(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var in struct {
		Name string `json:"name"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = "App Password"
	}
	raw := middleware.RandomToken(32)
	tok := models.Token{
		ID:         uuid.Must(uuid.NewV7()).String(),
		TeamID:     cl.TeamID,
		Kind:       "app_password",
		SubjectID:  cl.UserID,
		Name:       in.Name,
		TokenHash:  middleware.HashToken(raw),
		TokenValue: raw, // plaintext so it can be copied later from the list
		IP:         c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	}
	if err := h.app.DB.Create(&tok).Error; err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	httpx.OK(c, gin.H{"id": tok.ID, "name": tok.Name, "token": raw, "createdAt": tok.CreatedAt})
}

func (h *Handler) listAppPasswords(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var tokens []models.Token
	h.app.DB.Where("subject_id = ? AND kind = ? AND revoked_at IS NULL", cl.UserID, "app_password").
		Order("created_at DESC").Find(&tokens)
	out := make([]tokenEntry, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, tokenEntry{
			ID: t.ID, Kind: t.Kind, Name: t.Name, Token: t.TokenValue, IP: t.IP,
			UserAgent: t.UserAgent, ExpiresAt: t.ExpiresAt,
			LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt,
		})
	}
	httpx.OK(c, out)
}

func (h *Handler) revokeAppPassword(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	tokenID := c.Param("id")
	var tok models.Token
	if err := h.app.DB.Where("id = ? AND subject_id = ? AND kind = ? AND revoked_at IS NULL",
		tokenID, cl.UserID, "app_password").First(&tok).Error; err != nil {
		httpx.ErrT(c, 404, "not_found")
		return
	}
	now := time.Now()
	h.app.DB.Model(&tok).Update("revoked_at", now)
	httpx.OK(c, gin.H{"ok": true})
}
