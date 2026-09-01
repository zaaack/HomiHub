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
	g.POST("/auth/switch-family", auth, h.switchFamily)
	g.PATCH("/auth/profile", auth, h.updateProfile)
	g.POST("/auth/logout", auth, h.logout)
}

type loginResp struct {
	Token  string        `json:"token"`
	User   models.User   `json:"user"`
	Family models.Family `json:"family"`
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
	FamilyName string `json:"familyName"`
}

func (h *Handler) register(c *gin.Context) {
	var in registerInput
	if !httpx.Bind(c, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	in.FamilyName = strings.TrimSpace(in.FamilyName)
	if in.Name == "" || in.Email == "" || !passwordStrong(in.Password) {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	famName := in.FamilyName
	if famName == "" {
		famName = in.Name + " 的空间"
	}
	err = h.app.DB.Transaction(func(tx *gorm.DB) error {
		fam := models.Family{
			ID:            uuid.Must(uuid.NewV7()).String(),
			Name:          famName,
			CalendarToken: middleware.RandomToken(16),
		}
		user := models.User{
			ID:           uuid.Must(uuid.NewV7()).String(),
			FamilyID:     fam.ID,
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
		return tx.Create(&models.UserFamily{UserID: user.ID, FamilyID: fam.ID, Role: middleware.RoleParent}).Error
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
	var fam models.Family
	if err := h.app.DB.First(&fam, "id = ?", user.FamilyID).Error; err != nil {
		httpx.UnauthorizedT(c, "unauthorized")
		return
	}
	var uf models.UserFamily
	if err := h.app.DB.Where("user_id = ? AND family_id = ?", user.ID, fam.ID).First(&uf).Error; err == nil {
		user.Role = uf.Role
	}
	token, err := middleware.CreateToken(h.app.DB, fam.ID, "user", user.ID, "登录会话", c.ClientIP(), c.Request.UserAgent(), sessionTTL)
	if err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	httpx.OK(c, loginResp{Token: token, User: user, Family: fam})
}

func (h *Handler) me(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var user models.User
	if err := middleware.DB(c).First(&user, "id = ?", cl.UserID).Error; err != nil {
		httpx.UnauthorizedT(c, "unauthorized")
		return
	}
	var fam models.Family
	middleware.DB(c).First(&fam, "id = ?", cl.FamilyID)
	httpx.OK(c, gin.H{"user": user, "family": fam})
}

type familyEntry struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	IsHome bool   `json:"isHome"`
}

func (h *Handler) listFamilies(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var rows []models.UserFamily
	if err := h.app.DB.Where("user_id = ?", cl.UserID).Find(&rows).Error; err != nil {
		httpx.ErrT(c, 500, "query_failed")
		return
	}
	out := make([]familyEntry, 0, len(rows))
	for _, r := range rows {
		var fam models.Family
		if err := h.app.DB.First(&fam, "id = ?", r.FamilyID).Error; err != nil {
			continue
		}
		out = append(out, familyEntry{ID: fam.ID, Name: fam.Name, Role: r.Role, IsHome: r.FamilyID == cl.FamilyID})
	}
	httpx.OK(c, out)
}

func (h *Handler) switchFamily(c *gin.Context) {
	var in struct {
		FamilyID string `json:"familyId"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	cl := middleware.ClaimsOf(c)
	var uf models.UserFamily
	if err := h.app.DB.Where("user_id = ? AND family_id = ?", cl.UserID, in.FamilyID).First(&uf).Error; err != nil {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	var user models.User
	if err := h.app.DB.First(&user, "id = ?", cl.UserID).Error; err != nil {
		httpx.UnauthorizedT(c, "unauthorized")
		return
	}
	user.FamilyID = uf.FamilyID
	user.Role = uf.Role
	if err := h.app.DB.Save(&user).Error; err != nil {
		httpx.ErrT(c, 500, "save_failed")
		return
	}
	var fam models.Family
	h.app.DB.First(&fam, "id = ?", uf.FamilyID)
	token, err := middleware.CreateToken(h.app.DB, fam.ID, "user", user.ID, "登录会话", c.ClientIP(), c.Request.UserAgent(), sessionTTL)
	if err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	httpx.OK(c, loginResp{Token: token, User: user, Family: fam})
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
