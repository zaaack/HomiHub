package modulefamily

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
	moduleauth "homihub/backend/internal/modules/auth"
)

type Handler struct {
	app *modules.App
}

func (h *Handler) ID() string { return "family" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	return nil
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.GET("/family", auth, h.get)
	g.PATCH("/family", auth, middleware.RequireParent(), h.rename)
	g.POST("/family/reset-calendar-token", auth, middleware.RequireParent(), h.resetCalendarToken)
	g.GET("/family/members", auth, h.members)
	g.POST("/invites", auth, middleware.RequireParent(), h.createInvite)
	g.GET("/invites", auth, middleware.RequireParent(), h.listInvites)
	g.GET("/invites/info", h.inviteInfo)
	g.POST("/invites/join", auth, h.join)
}

func (h *Handler) get(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var fam models.Family
	if err := h.app.DB.First(&fam, "id = ?", cl.FamilyID).Error; err != nil {
		httpx.NotFoundT(c, "family_not_found")
		return
	}
	httpx.OK(c, fam)
}

func (h *Handler) rename(c *gin.Context) {
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
	cl := middleware.ClaimsOf(c)
	if err := h.app.DB.Model(&models.Family{}).Where("id = ?", cl.FamilyID).Update("name", in.Name).Error; err != nil {
		httpx.ErrT(c, 500, "save_failed")
		return
	}
	var fam models.Family
	h.app.DB.First(&fam, "id = ?", cl.FamilyID)
	httpx.OK(c, fam)
}

func (h *Handler) resetCalendarToken(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	token := middleware.RandomToken(16)
	if err := h.app.DB.Model(&models.Family{}).Where("id = ?", cl.FamilyID).Update("calendar_token", token).Error; err != nil {
		httpx.ErrT(c, 500, "save_failed")
		return
	}
	httpx.OK(c, gin.H{"calendarToken": token})
}

type memberView struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	IsOwner   bool   `json:"isOwner"`
	CreatedAt time.Time `json:"createdAt"`
}

func (h *Handler) members(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var rows []models.UserFamily
	if err := middleware.DB(c).Find(&rows).Error; err != nil {
		httpx.ErrT(c, 500, "query_failed")
		return
	}
	var fam models.Family
	h.app.DB.First(&fam, "id = ?", cl.FamilyID)
	out := make([]memberView, 0, len(rows))
	for _, r := range rows {
		var u models.User
		if err := h.app.DB.First(&u, "id = ?", r.UserID).Error; err != nil {
			continue
		}
		out = append(out, memberView{
			ID: u.ID, Email: u.Email, Name: u.Name, Role: r.Role,
			IsOwner: u.ID == fam.OwnerID, CreatedAt: u.CreatedAt,
		})
	}
	httpx.OK(c, out)
}

type inviteView struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *Handler) createInvite(c *gin.Context) {
	var in struct {
		Role string `json:"role"`
	}
	_ = c.ShouldBindJSON(&in)
	if in.Role != middleware.RoleChild && in.Role != middleware.RoleParent {
		in.Role = middleware.RoleChild
	}
	cl := middleware.ClaimsOf(c)
	raw := middleware.RandomToken(24)
	inv := models.Invite{
		ID:        uuid.Must(uuid.NewV7()).String(),
		FamilyID:  cl.FamilyID,
		TokenHash: middleware.HashToken(raw),
		Role:      in.Role,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := middleware.DB(c).Create(&inv).Error; err != nil {
		httpx.ErrT(c, 500, "create_failed")
		return
	}
	httpx.Created(c, inviteView{ID: inv.ID, Role: inv.Role, Link: raw, ExpiresAt: inv.ExpiresAt})
}

func (h *Handler) listInvites(c *gin.Context) {
	var invs []models.Invite
	if err := middleware.DB(c).Where("used_at IS NULL AND expires_at > ?", time.Now()).Find(&invs).Error; err != nil {
		httpx.ErrT(c, 500, "query_failed")
		return
	}
	out := make([]inviteView, 0, len(invs))
	for _, inv := range invs {
		out = append(out, inviteView{ID: inv.ID, Role: inv.Role, ExpiresAt: inv.ExpiresAt})
	}
	httpx.OK(c, out)
}

func (h *Handler) inviteInfo(c *gin.Context) {
	raw := c.Query("token")
	if raw == "" {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	var inv models.Invite
	if err := h.app.DB.Where("token_hash = ?", middleware.HashToken(raw)).First(&inv).Error; err != nil ||
		inv.UsedAt != nil || time.Now().After(inv.ExpiresAt) {
		httpx.NotFoundT(c, "invite_invalid")
		return
	}
	var fam models.Family
	if err := h.app.DB.First(&fam, "id = ?", inv.FamilyID).Error; err != nil {
		httpx.NotFoundT(c, "invite_invalid")
		return
	}
	httpx.OK(c, gin.H{"familyName": fam.Name, "role": inv.Role})
}

func (h *Handler) join(c *gin.Context) {
	var in struct {
		Code string `json:"code"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	cl := middleware.ClaimsOf(c)
	var inv models.Invite
	if err := h.app.DB.Where("token_hash = ?", middleware.HashToken(in.Code)).First(&inv).Error; err != nil ||
		inv.UsedAt != nil || time.Now().After(inv.ExpiresAt) {
		httpx.BadRequestT(c, "invite_invalid")
		return
	}
	var user models.User
	if err := h.app.DB.First(&user, "id = ?", cl.UserID).Error; err != nil {
		httpx.UnauthorizedT(c, "unauthorized")
		return
	}
	now := time.Now()
	err := h.app.DB.Transaction(func(tx *gorm.DB) error {
		var n int64
		tx.Model(&models.UserFamily{}).Where("user_id = ? AND family_id = ?", user.ID, inv.FamilyID).Count(&n)
		if n == 0 {
			if err := tx.Create(&models.UserFamily{UserID: user.ID, FamilyID: inv.FamilyID, Role: inv.Role}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.Invite{}).Where("id = ?", inv.ID).Update("used_at", &now).Error
	})
	if err != nil {
		httpx.ErrT(c, 500, "save_failed")
		return
	}
	user.FamilyID = inv.FamilyID
	user.Role = inv.Role
	if err := h.app.DB.Save(&user).Error; err != nil {
		httpx.ErrT(c, 500, "save_failed")
		return
	}
	var fam models.Family
	h.app.DB.First(&fam, "id = ?", inv.FamilyID)
	token, err := middleware.CreateToken(h.app.DB, fam.ID, "user", user.ID, "登录会话", c.ClientIP(), c.Request.UserAgent(), 30*24*time.Hour)
	if err != nil {
		httpx.ErrT(c, 500, "internal_error")
		return
	}
	httpx.OK(c, moduleauth.LoginResp{Token: token, User: user, Family: fam})
}
