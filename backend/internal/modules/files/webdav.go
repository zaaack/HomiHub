package modulefiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/emersion/go-webdav"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	modulecalendar "homihub/backend/internal/modules/calendar"
)

// DAVHandler returns an http.Handler serving WebDAV on /dav/files. It must be
// mounted behind the calendar davAuth (which injects the DavSession).
func (h *Handler) DAVHandler() http.Handler {
	return &webdav.Handler{FileSystem: &filesWebDAV{h: h}}
}

func (h *Handler) session(ctx context.Context) (*modulecalendar.DavSession, error) {
	s := modulecalendar.SessionFromContext(ctx)
	if s == nil {
		return nil, errors.New("no dav session")
	}
	return s, nil
}

// parseFilesPath splits a /dav/files path into scope and remaining segments.
func parseFilesPath(name string) (scope string, segs []string, err error) {
	name = strings.TrimSuffix(path.Clean(name), "/")
	if name == "/dav/files" {
		return "", nil, nil
	}
	rel := strings.TrimPrefix(name, "/dav/files/")
	if rel == name {
		return "", nil, fs.ErrNotExist
	}
	parts := strings.Split(rel, "/")
	scope = parts[0]
	if scope != models.ScopePublic && scope != models.ScopePersonal {
		return "", nil, fs.ErrNotExist
	}
	segs = parts[1:]
	return scope, segs, nil
}

func fileEtag(id, updatedAt string, size int64) string {
	sum := sha256.Sum256([]byte(id + updatedAt))
	return hex.EncodeToString(sum[:8]) + "-" + fmt.Sprintf("%d", size)
}

type filesWebDAV struct {
	h *Handler
}

func (w *filesWebDAV) session(ctx context.Context) (*modulecalendar.DavSession, error) {
	return w.h.session(ctx)
}

func (w *filesWebDAV) db(ctx context.Context) (*gorm.DB, error) {
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	return middleware.ScopedDB(w.h.app.DB, s.Family.ID), nil
}

// resolveFolder walks folder names relative to the scope root to a FolderID.
func (w *filesWebDAV) resolveFolder(ctx context.Context, scope string, names []string) (string, error) {
	if len(names) == 0 {
		return "", nil
	}
	s, err := w.session(ctx)
	if err != nil {
		return "", err
	}
	db, err := w.db(ctx)
	if err != nil {
		return "", err
	}
	folderID := ""
	for _, n := range names {
		var f models.FileFolder
		q := db.Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, n)
		if scope == models.ScopePersonal {
			q = q.Where("owner_id = ?", s.User.ID)
		}
		if err := q.First(&f).Error; err != nil {
			return "", fs.ErrNotExist
		}
		folderID = f.ID
	}
	return folderID, nil
}

func (w *filesWebDAV) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return nil, err
	}
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	db, err := w.db(ctx)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return emptyFile{}, nil
	}
	folderID, err := w.resolveFolder(ctx, scope, segs[:len(segs)-1])
	if err != nil {
		return nil, err
	}
	leaf := segs[len(segs)-1]
	var folder models.FileFolder
	q := db.Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.First(&folder).Error; err == nil {
		return emptyFile{}, nil
	}
	var file models.File
	qf := db.Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.First(&file).Error; err != nil {
		return nil, fs.ErrNotExist
	}
	return w.h.st.Open(ctx, contentKey(file.FamilyID, file.Scope, file.ID))
}

func (w *filesWebDAV) Stat(ctx context.Context, name string) (*webdav.FileInfo, error) {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return nil, err
	}
	if scope == "" {
		return &webdav.FileInfo{Path: name, IsDir: true, ModTime: time.Now()}, nil
	}
	if len(segs) == 0 {
		return &webdav.FileInfo{Path: name, IsDir: true, ModTime: time.Now()}, nil
	}
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	db, err := w.db(ctx)
	if err != nil {
		return nil, err
	}
	folderID, err := w.resolveFolder(ctx, scope, segs[:len(segs)-1])
	if err != nil {
		return nil, err
	}
	leaf := segs[len(segs)-1]
	var folder models.FileFolder
	q := db.Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.First(&folder).Error; err == nil {
		return &webdav.FileInfo{Path: name, IsDir: true, ModTime: folder.UpdatedAt}, nil
	}
	var file models.File
	qf := db.Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.First(&file).Error; err != nil {
		return nil, fs.ErrNotExist
	}
	mimeType := file.MimeType
	if mimeType == "" {
		mimeType = mime.TypeByExtension(path.Ext(file.Name))
	}
	return &webdav.FileInfo{
		Path:     name,
		Size:     file.Size,
		ModTime:  file.UpdatedAt,
		MIMEType: mimeType,
		ETag:     fileEtag(file.ID, file.UpdatedAt.String(), file.Size),
	}, nil
}

func (w *filesWebDAV) ReadDir(ctx context.Context, name string, recursive bool) ([]webdav.FileInfo, error) {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return nil, err
	}
	if scope == "" {
		return []webdav.FileInfo{
			{Path: path.Join(name, models.ScopePublic), IsDir: true, ModTime: time.Now()},
			{Path: path.Join(name, models.ScopePersonal), IsDir: true, ModTime: time.Now()},
		}, nil
	}
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	db, err := w.db(ctx)
	if err != nil {
		return nil, err
	}
	folderID, err := w.resolveFolder(ctx, scope, segs)
	if err != nil {
		return nil, err
	}
	var out []webdav.FileInfo
	var folders []models.FileFolder
	q := db.Where("scope = ? AND parent_id = ? AND deleted_at IS NULL", scope, folderID)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.Find(&folders).Error; err != nil {
		return nil, err
	}
	for i := range folders {
		out = append(out, webdav.FileInfo{
			Path: path.Join(name, folders[i].Name), IsDir: true, ModTime: folders[i].UpdatedAt,
		})
	}
	var files []models.File
	qf := db.Where("scope = ? AND folder_id = ? AND deleted_at IS NULL", scope, folderID)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.Find(&files).Error; err != nil {
		return nil, err
	}
	for i := range files {
		mimeType := files[i].MimeType
		if mimeType == "" {
			mimeType = mime.TypeByExtension(path.Ext(files[i].Name))
		}
		out = append(out, webdav.FileInfo{
			Path:     path.Join(name, files[i].Name),
			Size:     files[i].Size,
			ModTime:  files[i].UpdatedAt,
			MIMEType: mimeType,
			ETag:     fileEtag(files[i].ID, files[i].UpdatedAt.String(), files[i].Size),
		})
	}
	return out, nil
}

func (w *filesWebDAV) Create(ctx context.Context, name string, body io.ReadCloser, opts *webdav.CreateOptions) (*webdav.FileInfo, bool, error) {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return nil, false, err
	}
	s, err := w.session(ctx)
	if err != nil {
		return nil, false, err
	}
	db, err := w.db(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(segs) == 0 {
		return nil, false, fs.ErrInvalid
	}
	folderID, err := w.resolveFolder(ctx, scope, segs[:len(segs)-1])
	if err != nil {
		return nil, false, err
	}
	leaf := segs[len(segs)-1]
	var existing models.File
	q := db.Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	created := true
	if err := q.First(&existing).Error; err == nil {
		created = false
	}
	fileID := existing.ID
	if fileID == "" {
		fileID = uuid.Must(uuid.NewV7()).String()
	}
	f := models.File{
		ID:       fileID,
		FamilyID: s.Family.ID,
		Scope:    scope,
		OwnerID:  s.User.ID,
		Name:     leaf,
		MimeType: mime.TypeByExtension(path.Ext(leaf)),
		FolderID: folderID,
	}
	if err := w.h.st.Save(ctx, contentKey(f.FamilyID, f.Scope, f.ID), body); err != nil {
		return nil, false, err
	}
	if created {
		if err := db.Create(&f).Error; err != nil {
			return nil, false, err
		}
	} else {
		f.ID = existing.ID
		if err := db.Model(&models.File{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"size": f.Size, "mime_type": f.MimeType, "updated_at": time.Now(),
		}).Error; err != nil {
			return nil, false, err
		}
		f.Size = existing.Size
	}
	fi := &webdav.FileInfo{
		Path:     name,
		Size:     f.Size,
		ModTime:  time.Now(),
		MIMEType: f.MimeType,
		ETag:     fileEtag(f.ID, time.Now().String(), f.Size),
	}
	return fi, created, nil
}

func (w *filesWebDAV) RemoveAll(ctx context.Context, name string, opts *webdav.RemoveAllOptions) error {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return err
	}
	db, err := w.db(ctx)
	if err != nil {
		return err
	}
	if len(segs) == 0 {
		return nil
	}
	folderID, err := w.resolveFolder(ctx, scope, segs[:len(segs)-1])
	if err != nil {
		return err
	}
	leaf := segs[len(segs)-1]
	now := time.Now()
	db.Model(&models.File{}).Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf).
		Update("deleted_at", &now)
	db.Model(&models.FileFolder{}).Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf).
		Update("deleted_at", &now)
	return nil
}

func (w *filesWebDAV) Mkdir(ctx context.Context, name string) error {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return err
	}
	s, err := w.session(ctx)
	if err != nil {
		return err
	}
	db, err := w.db(ctx)
	if err != nil {
		return err
	}
	if len(segs) == 0 {
		return fs.ErrInvalid
	}
	folderID, err := w.resolveFolder(ctx, scope, segs[:len(segs)-1])
	if err != nil {
		return err
	}
	folder := models.FileFolder{
		ID:       uuid.Must(uuid.NewV7()).String(),
		FamilyID: s.Family.ID,
		Scope:    scope,
		OwnerID:  s.User.ID,
		ParentID: folderID,
		Name:     segs[len(segs)-1],
	}
	return db.Create(&folder).Error
}

func (w *filesWebDAV) Copy(ctx context.Context, src, dst string, options *webdav.CopyOptions) (bool, error) {
	rc, err := w.Open(ctx, src)
	if err != nil {
		return false, err
	}
	defer rc.Close()
	fi, created, err := w.Create(ctx, dst, rc, nil)
	if err != nil {
		return false, err
	}
	_ = fi
	return created, nil
}

func (w *filesWebDAV) Move(ctx context.Context, src, dst string, options *webdav.MoveOptions) (bool, error) {
	created, err := w.Copy(ctx, src, dst, nil)
	if err != nil {
		return false, err
	}
	if err := w.RemoveAll(ctx, src, nil); err != nil {
		return false, err
	}
	return created, nil
}

// emptyFile is a no-op ReadCloser for directory opens.
type emptyFile struct{}

func (emptyFile) Read(p []byte) (int, error)         { return 0, io.EOF }
func (emptyFile) Close() error                       { return nil }
