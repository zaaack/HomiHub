package modulefiles

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"homihub/backend/internal/httpx"
	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	"homihub/backend/internal/modules"
	"homihub/backend/internal/storage"
)

type Handler struct {
	app *modules.App
	st  storage.Driver
}

func (h *Handler) ID() string { return "files" }

func (h *Handler) Init(app *modules.App) error {
	h.app = app
	h.st = &storage.Local{Root: app.Config.StorageDir}
	return nil
}

func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	auth := middleware.Auth(h.app.DB, h.app.Config.JWTSecret)
	g.GET("/files", auth, h.list)
	g.POST("/files", auth, h.create)
	g.GET("/files/:id", auth, h.detail)
	g.GET("/files/:id/content", auth, h.content)
	g.PATCH("/files/:id", auth, h.update)
	g.DELETE("/files/:id", auth, h.delete)
	g.GET("/files/folders", auth, h.listFolders)
	g.POST("/files/folders", auth, h.createFolder)
	g.PATCH("/files/folders/:id", auth, h.updateFolder)
	g.DELETE("/files/folders/:id", auth, h.deleteFolder)
}

func contentKey(teamID, scope, fileID string) string {
	return fmt.Sprintf("files/%s/%s/%s", teamID, scope, fileID)
}

func (h *Handler) canView(c *gin.Context, f *models.File) bool {
	cl := middleware.ClaimsOf(c)
	if f.Scope == models.ScopePublic {
		return true
	}
	return f.OwnerID == cl.UserID
}

func (h *Handler) canEdit(c *gin.Context, f *models.File) bool {
	cl := middleware.ClaimsOf(c)
	if cl.Role == middleware.RoleParent {
		return true
	}
	return f.OwnerID == cl.UserID
}

func (h *Handler) loadFile(c *gin.Context, id string) (*models.File, bool) {
	var f models.File
	if err := middleware.DB(c).First(&f, "id = ?", id).Error; err != nil {
		httpx.NotFoundT(c, "file_not_found")
		return nil, false
	}
	return &f, true
}

func (h *Handler) list(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	scope := c.DefaultQuery("scope", models.ScopePublic)
	folderID := c.Query("folderId")
	var files []models.File
	q := middleware.DB(c).Where("scope = ? AND deleted_at IS NULL AND attachment_of = ''", scope)
	if folderID == "" {
		q = q.Where("folder_id = ''")
	} else {
		q = q.Where("folder_id = ?", folderID)
	}
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", cl.UserID)
	}
	if err := q.Order("created_at desc").Find(&files).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	httpx.OK(c, files)
}

func (h *Handler) create(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	scope := c.DefaultPostForm("scope", models.ScopePublic)
	if scope != models.ScopePublic && scope != models.ScopePersonal {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	folderID := c.PostForm("folderId")
	fileHeader, err := c.FormFile("file")
	if err != nil {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	fileID := uuid.Must(uuid.NewV7()).String()
	key := contentKey(cl.TeamID, scope, fileID)
	part, err := fileHeader.Open()
	if err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	defer part.Close()
	if err := h.st.Save(c.Request.Context(), key, part); err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	f := models.File{
		ID:        fileID,
		TeamID:  cl.TeamID,
		Scope:     scope,
		OwnerID:   cl.UserID,
		Name:      fileHeader.Filename,
		MimeType:  fileHeader.Header.Get("Content-Type"),
		Size:      fileHeader.Size,
		FolderID:  folderID,
	}
	if err := middleware.DB(c).Create(&f).Error; err != nil {
		h.st.Delete(c.Request.Context(), key)
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	httpx.Created(c, f)
}

func (h *Handler) detail(c *gin.Context) {
	f, ok := h.loadFile(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.canView(c, f) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	httpx.OK(c, f)
}

func (h *Handler) content(c *gin.Context) {
	f, ok := h.loadFile(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.canView(c, f) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	rc, err := h.st.Open(c.Request.Context(), contentKey(f.TeamID, f.Scope, f.ID))
	if err != nil {
		httpx.ErrT(c, http.StatusNotFound, "file_not_found")
		return
	}
	defer rc.Close()
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(c.Writer, c.Request, f.Name, f.UpdatedAt, rs)
		return
	}
	c.DataFromReader(http.StatusOK, f.Size, f.MimeType, rc, nil)
}

func (h *Handler) update(c *gin.Context) {
	f, ok := h.loadFile(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.canEdit(c, f) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	var in struct {
		Name     string `json:"name"`
		FolderID string `json:"folderId"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	updates := map[string]any{}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	if in.FolderID != "" {
		updates["folder_id"] = in.FolderID
	} else if c.Request.ContentLength > 0 && !strings.Contains(c.ContentType(), "multipart") {
		updates["folder_id"] = ""
	}
	if len(updates) == 0 {
		httpx.OK(c, f)
		return
	}
	if err := middleware.DB(c).Model(&models.File{}).Where("id = ?", f.ID).Updates(updates).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	var updated models.File
	middleware.DB(c).First(&updated, "id = ?", f.ID)
	httpx.OK(c, updated)
}

func (h *Handler) delete(c *gin.Context) {
	f, ok := h.loadFile(c, c.Param("id"))
	if !ok {
		return
	}
	if !h.canEdit(c, f) {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	now := time.Now()
	if err := middleware.DB(c).Model(&models.File{}).Where("id = ?", f.ID).Update("deleted_at", &now).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}

func (h *Handler) listFolders(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	scope := c.DefaultQuery("scope", models.ScopePublic)
	parentID := c.Query("parentId")
	var folders []models.FileFolder
	q := middleware.DB(c).Where("scope = ? AND deleted_at IS NULL", scope)
	if parentID == "" {
		q = q.Where("parent_id = ''")
	} else {
		q = q.Where("parent_id = ?", parentID)
	}
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", cl.UserID)
	}
	if err := q.Order("created_at desc").Find(&folders).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "query_failed")
		return
	}
	httpx.OK(c, folders)
}

func (h *Handler) createFolder(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var in struct {
		Name     string `json:"name"`
		Scope    string `json:"scope"`
		ParentID string `json:"parentId"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		httpx.BadRequestT(c, "bad_request")
		return
	}
	if in.Scope != models.ScopePublic && in.Scope != models.ScopePersonal {
		in.Scope = models.ScopePublic
	}
	folder := models.FileFolder{
		ID:       uuid.Must(uuid.NewV7()).String(),
		TeamID: cl.TeamID,
		Scope:    in.Scope,
		OwnerID:  cl.UserID,
		ParentID: in.ParentID,
		Name:     in.Name,
	}
	if err := middleware.DB(c).Create(&folder).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "create_failed")
		return
	}
	httpx.Created(c, folder)
}

func (h *Handler) updateFolder(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var folder models.FileFolder
	if err := middleware.DB(c).First(&folder, "id = ?", c.Param("id")).Error; err != nil {
		httpx.NotFoundT(c, "folder_not_found")
		return
	}
	if folder.OwnerID != cl.UserID && cl.Role != middleware.RoleParent {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
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
	if err := middleware.DB(c).Model(&models.FileFolder{}).Where("id = ?", folder.ID).Update("name", in.Name).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "save_failed")
		return
	}
	var updated models.FileFolder
	middleware.DB(c).First(&updated, "id = ?", folder.ID)
	httpx.OK(c, updated)
}

func (h *Handler) deleteFolder(c *gin.Context) {
	cl := middleware.ClaimsOf(c)
	var folder models.FileFolder
	if err := middleware.DB(c).First(&folder, "id = ?", c.Param("id")).Error; err != nil {
		httpx.NotFoundT(c, "folder_not_found")
		return
	}
	if folder.OwnerID != cl.UserID && cl.Role != middleware.RoleParent {
		httpx.ForbiddenT(c, "forbidden")
		return
	}
	now := time.Now()
	if err := middleware.DB(c).Model(&models.FileFolder{}).Where("id = ?", folder.ID).Update("deleted_at", &now).Error; err != nil {
		httpx.ErrT(c, http.StatusInternalServerError, "delete_failed")
		return
	}
	httpx.OK(c, gin.H{"ok": true})
}
