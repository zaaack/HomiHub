package modulefiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/z/go-webdavp"
	"gorm.io/gorm"

	"homihub/backend/internal/middleware"
	"homihub/backend/internal/models"
	modulecalendar "homihub/backend/internal/modules/calendar"
)

// DAVHandler returns an http.Handler serving WebDAV on /dav/files. It must be
// mounted behind the calendar davAuth (which injects the DavSession).
func (h *Handler) DAVHandler() http.Handler {
	return &webdav.Handler{
		FileSystem: &filesWebDAV{h: h},
		LockSystem: webdav.NewMemLS(),
	}
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
	return middleware.ScopedDB(w.h.app.DB, s.Team.ID), nil
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
	folderID := ""
	for _, n := range names {
		db, err := w.db(ctx)
		if err != nil {
			return "", err
		}
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

// xfileInfo implements os.FileInfo plus the optional ETager/ContentTyper
// interfaces used by the x/net/webdav handler for PROPFIND.
type xfileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	etag    string
	ctype   string
}

func (i xfileInfo) Name() string       { return i.name }
func (i xfileInfo) Size() int64        { return i.size }
func (i xfileInfo) Mode() os.FileMode  { return i.mode }
func (i xfileInfo) ModTime() time.Time { return i.modTime }
func (i xfileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i xfileInfo) Sys() interface{}   { return nil }
func (i xfileInfo) ETag(context.Context) (string, error) {
	return i.etag, nil
}
func (i xfileInfo) ContentType(context.Context) (string, error) {
	return i.ctype, nil
}

// xfile implements webdav.File for read/write/directory access.
type xfile struct {
	w      *filesWebDAV
	ctx    context.Context
	name   string // full /dav/files/... path
	scope  string
	segs   []string
	isDir  bool
	isRoot bool // virtual scope root (e.g. /dav/files or /dav/files/public)

	// resource identity (for persistence + dead props)
	teamID   string
	ownerID  string
	fileID   string // non-empty when backing a file
	folderID string // own folder id when backing a folder; else parent folder id

	info xfileInfo

	// read mode: seekable content (spooled to temp file, or direct from store)
	rs    io.ReadSeeker
	spool *os.File
	rcloser io.Closer

	// write mode
	wbuf    *os.File
	wsize   int64
	created bool

	// dir mode: snapshot of children
	children []os.FileInfo
	dirIdx   int
}

// --- webdav.FileSystem ---

func (w *filesWebDAV) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
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
	leaf := segs[len(segs)-1]
	if exists, err := w.resourceExists(ctx, scope, folderID, leaf, s.User.ID); err != nil {
		return err
	} else if exists {
		return fs.ErrExist
	}
	folder := models.FileFolder{
		ID:       uuid.Must(uuid.NewV7()).String(),
		TeamID:   s.Team.ID,
		Scope:    scope,
		OwnerID:  s.User.ID,
		ParentID: folderID,
		Name:     leaf,
	}
	return db.Create(&folder).Error
}

func (w *filesWebDAV) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return nil, err
	}
	if scope == "" || len(segs) == 0 {
		// virtual scope root: only readable
		if flag&(os.O_WRONLY|os.O_RDWR) != 0 {
			return nil, fs.ErrPermission
		}
		f, err := w.openScopeRoot(ctx, scope, name)
		if err != nil {
			return nil, err
		}
		return f, nil
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

	// existing folder?
	var folder models.FileFolder
	q := db.Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.First(&folder).Error; err == nil {
		if flag&(os.O_WRONLY|os.O_RDWR) != 0 && flag&os.O_CREATE == 0 {
			// PROPPATCH on a folder: metadata-only file
			return w.folderMetaFile(ctx, scope, segs, &folder, name), nil
		}
		return w.openDir(ctx, scope, segs, &folder, name)
	}

	// existing file?
	db, err = w.db(ctx)
	if err != nil {
		return nil, err
	}
	var file models.File
	qf := db.Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	found := qf.First(&file).Error == nil

	if flag&(os.O_WRONLY|os.O_RDWR) != 0 && flag&os.O_CREATE != 0 {
		// write mode (PUT / COPY dst / LOCK-create)
		return w.openWrite(ctx, scope, segs, folderID, leaf, s, db, &file, found)
	}
	if !found {
		return nil, fs.ErrNotExist
	}
	// read mode (GET / PROPFIND / COPY src / PROPPATCH on file)
	if flag&os.O_WRONLY != 0 {
		return w.fileMetaFile(ctx, scope, segs, &file, name), nil
	}
	return w.openRead(ctx, scope, segs, &file, name)
}

func (w *filesWebDAV) RemoveAll(ctx context.Context, name string) error {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return err
	}
	if scope == "" || len(segs) == 0 {
		return fs.ErrInvalid
	}
	s, err := w.session(ctx)
	if err != nil {
		return err
	}
	folderID, err := w.resolveFolder(ctx, scope, segs[:len(segs)-1])
	if err != nil {
		return err
	}
	leaf := segs[len(segs)-1]
	now := time.Now()
	db, err := w.db(ctx)
	if err != nil {
		return err
	}
	// soft-delete a file if present
	qf := db.Model(&models.File{}).Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.Update("deleted_at", &now).Error; err != nil {
		return err
	}
	// soft-delete a folder (recursively) if present
	db, err = w.db(ctx)
	if err != nil {
		return err
	}
	q := db.Model(&models.FileFolder{}).Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	var f models.FileFolder
	if err := q.First(&f).Error; err == nil {
		if err := w.removeFolderTree(ctx, scope, f.ID, s.User.ID); err != nil {
			return err
		}
	}
	return nil
}

// removeFolderTree soft-deletes a folder and all descendants.
func (w *filesWebDAV) removeFolderTree(ctx context.Context, scope, folderID, ownerID string) error {
	now := time.Now()
	var sub []models.FileFolder
	db, err := w.db(ctx)
	if err != nil {
		return err
	}
	q := db.Where("scope = ? AND parent_id = ? AND deleted_at IS NULL", scope, folderID)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", ownerID)
	}
	if err := q.Find(&sub).Error; err != nil {
		return err
	}
	for _, c := range sub {
		if err := w.removeFolderTree(ctx, scope, c.ID, ownerID); err != nil {
			return err
		}
	}
	db, err = w.db(ctx)
	if err != nil {
		return err
	}
	if err := db.Model(&models.File{}).Where("scope = ? AND folder_id = ? AND deleted_at IS NULL", scope, folderID).
		Update("deleted_at", &now).Error; err != nil {
		return err
	}
	db, err = w.db(ctx)
	if err != nil {
		return err
	}
	return db.Model(&models.FileFolder{}).Where("id = ?", folderID).Update("deleted_at", &now).Error
}

func (w *filesWebDAV) Rename(ctx context.Context, oldName, newName string) error {
	oScope, oSegs, err := parseFilesPath(oldName)
	if err != nil {
		return err
	}
	nScope, nSegs, err := parseFilesPath(newName)
	if err != nil {
		return err
	}
	if oScope == "" || len(oSegs) == 0 || nScope == "" || len(nSegs) == 0 {
		return fs.ErrInvalid
	}
	if oScope != nScope {
		return fs.ErrInvalid
	}
	s, err := w.session(ctx)
	if err != nil {
		return err
	}
	db, err := w.db(ctx)
	if err != nil {
		return err
	}
	// source parent + leaf
	oParent, err := w.resolveFolder(ctx, oScope, oSegs[:len(oSegs)-1])
	if err != nil {
		return err
	}
	oLeaf := oSegs[len(oSegs)-1]
	// destination parent + leaf
	nParent, err := w.resolveFolder(ctx, nScope, nSegs[:len(nSegs)-1])
	if err != nil {
		return err
	}
	nLeaf := nSegs[len(nSegs)-1]

	// file?
	var file models.File
	qf := db.Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", oScope, oParent, oLeaf)
	if oScope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.First(&file).Error; err == nil {
		udb, err := w.db(ctx)
		if err != nil {
			return err
		}
		return udb.Model(&models.File{}).Where("id = ?", file.ID).
			Updates(map[string]any{"folder_id": nParent, "name": nLeaf}).Error
	}
	// folder?
	var folder models.FileFolder
	fdb, err := w.db(ctx)
	if err != nil {
		return err
	}
	q := fdb.Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", oScope, oParent, oLeaf)
	if oScope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.First(&folder).Error; err != nil {
		return fs.ErrNotExist
	}
	udb, err := w.db(ctx)
	if err != nil {
		return err
	}
	return udb.Model(&models.FileFolder{}).Where("id = ?", folder.ID).
		Updates(map[string]any{"parent_id": nParent, "name": nLeaf}).Error
}

func (w *filesWebDAV) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	scope, segs, err := parseFilesPath(name)
	if err != nil {
		return nil, err
	}
	if scope == "" || len(segs) == 0 {
		return xfileInfo{
			name:    path.Base(strings.TrimSuffix(name, "/")),
			mode:    os.ModeDir | 0o755,
			modTime: time.Now(),
		}, nil
	}
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	folderID, err := w.resolveFolder(ctx, scope, segs[:len(segs)-1])
	if err != nil {
		return nil, err
	}
	leaf := segs[len(segs)-1]
	db, err := w.db(ctx)
	if err != nil {
		return nil, err
	}
	// folder?
	var folder models.FileFolder
	q := db.Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.First(&folder).Error; err == nil {
		return xfileInfo{
			name:    leaf,
			mode:    os.ModeDir | 0o755,
			modTime: folder.UpdatedAt,
			etag:    fileEtag(folder.ID, folder.UpdatedAt.String(), 0),
		}, nil
	}
	// file?
	db, err = w.db(ctx)
	if err != nil {
		return nil, err
	}
	var file models.File
	qf := db.Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.First(&file).Error; err != nil {
		return nil, fs.ErrNotExist
	}
	ctype := file.MimeType
	if ctype == "" {
		ctype = mime.TypeByExtension(path.Ext(file.Name))
	}
	return xfileInfo{
		name:    leaf,
		size:    file.Size,
		mode:    0o644,
		modTime: file.UpdatedAt,
		etag:    fileEtag(file.ID, file.UpdatedAt.String(), file.Size),
		ctype:   ctype,
	}, nil
}

// --- helpers ---

func (w *filesWebDAV) resourceExists(ctx context.Context, scope, folderID, leaf, ownerID string) (bool, error) {
	var n int64
	db, err := w.db(ctx)
	if err != nil {
		return false, err
	}
	q := db.Model(&models.FileFolder{}).Where("scope = ? AND parent_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", ownerID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	db, err = w.db(ctx)
	if err != nil {
		return false, err
	}
	q = db.Model(&models.File{}).Where("scope = ? AND folder_id = ? AND name = ? AND deleted_at IS NULL", scope, folderID, leaf)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", ownerID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// openScopeRoot builds a virtual directory File for /dav/files or a scope root.
func (w *filesWebDAV) openScopeRoot(ctx context.Context, scope, name string) (*xfile, error) {
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	f := &xfile{w: w, ctx: ctx, name: name, scope: scope, isDir: true, isRoot: true}
	if scope == "" {
		f.children = []os.FileInfo{
			xfileInfo{name: models.ScopePublic, mode: os.ModeDir | 0o755, modTime: time.Now()},
			xfileInfo{name: models.ScopePersonal, mode: os.ModeDir | 0o755, modTime: time.Now()},
		}
		f.folderID = ""
		return f, nil
	}
	db, err := w.db(ctx)
	if err != nil {
		return nil, err
	}
	var folders []models.FileFolder
	q := db.Where("scope = ? AND parent_id = '' AND deleted_at IS NULL", scope)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.Find(&folders).Error; err != nil {
		return nil, err
	}
	for i := range folders {
		f.children = append(f.children, xfileInfo{
			name: folders[i].Name, mode: os.ModeDir | 0o755,
			modTime: folders[i].UpdatedAt,
			etag:    fileEtag(folders[i].ID, folders[i].UpdatedAt.String(), 0),
		})
	}
	var files []models.File
	db, err = w.db(ctx)
	if err != nil {
		return nil, err
	}
	qf := db.Where("scope = ? AND folder_id = '' AND deleted_at IS NULL", scope)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.Find(&files).Error; err != nil {
		return nil, err
	}
	for i := range files {
		ctype := files[i].MimeType
		if ctype == "" {
			ctype = mime.TypeByExtension(path.Ext(files[i].Name))
		}
		f.children = append(f.children, xfileInfo{
			name: files[i].Name, size: files[i].Size, mode: 0o644,
			modTime: files[i].UpdatedAt,
			etag:    fileEtag(files[i].ID, files[i].UpdatedAt.String(), files[i].Size),
			ctype:   ctype,
		})
	}
	return f, nil
}

func (w *filesWebDAV) openDir(ctx context.Context, scope string, segs []string, folder *models.FileFolder, name string) (*xfile, error) {
	s, err := w.session(ctx)
	if err != nil {
		return nil, err
	}
	db, err := w.db(ctx)
	if err != nil {
		return nil, err
	}
	f := &xfile{
		w: w, ctx: ctx, name: name, scope: scope, segs: segs,
		isDir: true, folderID: folder.ID, ownerID: s.User.ID,
		info: xfileInfo{name: segs[len(segs)-1], mode: os.ModeDir | 0o755, modTime: folder.UpdatedAt},
	}
	var folders []models.FileFolder
	q := db.Where("scope = ? AND parent_id = ? AND deleted_at IS NULL", scope, folder.ID)
	if scope == models.ScopePersonal {
		q = q.Where("owner_id = ?", s.User.ID)
	}
	if err := q.Find(&folders).Error; err != nil {
		return nil, err
	}
	for i := range folders {
		f.children = append(f.children, xfileInfo{
			name: folders[i].Name, mode: os.ModeDir | 0o755,
			modTime: folders[i].UpdatedAt,
			etag:    fileEtag(folders[i].ID, folders[i].UpdatedAt.String(), 0),
		})
	}
	var files []models.File
	db, err = w.db(ctx)
	if err != nil {
		return nil, err
	}
	qf := db.Where("scope = ? AND folder_id = ? AND deleted_at IS NULL", scope, folder.ID)
	if scope == models.ScopePersonal {
		qf = qf.Where("owner_id = ?", s.User.ID)
	}
	if err := qf.Find(&files).Error; err != nil {
		return nil, err
	}
	for i := range files {
		ctype := files[i].MimeType
		if ctype == "" {
			ctype = mime.TypeByExtension(path.Ext(files[i].Name))
		}
		f.children = append(f.children, xfileInfo{
			name: files[i].Name, size: files[i].Size, mode: 0o644,
			modTime: files[i].UpdatedAt,
			etag:    fileEtag(files[i].ID, files[i].UpdatedAt.String(), files[i].Size),
			ctype:   ctype,
		})
	}
	return f, nil
}

// openRead opens file content for seekable reads. When the underlying store
// already returns a seekable reader it is used directly; otherwise the content
// is spooled to a temp file (no extra copy for Local).
func (w *filesWebDAV) openRead(ctx context.Context, scope string, segs []string, file *models.File, name string) (*xfile, error) {
	rc, err := w.h.st.Open(ctx, contentKey(file.TeamID, file.Scope, file.ID))
	if err != nil {
		return nil, err
	}
	ctype := file.MimeType
	if ctype == "" {
		ctype = mime.TypeByExtension(path.Ext(file.Name))
	}
	f := &xfile{
		w: w, ctx: ctx, name: name, scope: scope, segs: segs,
		fileID: file.ID, teamID: file.TeamID, ownerID: file.OwnerID,
		folderID: file.FolderID,
		info: xfileInfo{
			name: segs[len(segs)-1], size: file.Size, mode: 0o644,
			modTime: file.UpdatedAt, etag: fileEtag(file.ID, file.UpdatedAt.String(), file.Size),
			ctype: ctype,
		},
	}
	if rs, ok := rc.(io.ReadSeeker); ok {
		f.rs = rs
		f.rcloser = rc
		return f, nil
	}
	defer rc.Close()
	tmp, err := os.CreateTemp("", "homihub-dav-*")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(tmp, rc); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	f.rs = tmp
	f.spool = tmp
	return f, nil
}

// openWrite prepares a temp buffer for a PUT/COPY-dest/LOCK-create operation.
// The row is created/updated when the file is closed.
func (w *filesWebDAV) openWrite(ctx context.Context, scope string, segs []string, folderID, leaf string, s *modulecalendar.DavSession, db *gorm.DB, file *models.File, found bool) (*xfile, error) {
	tmp, err := os.CreateTemp("", "homihub-dav-*")
	if err != nil {
		return nil, err
	}
	f := &xfile{
		w: w, ctx: ctx, name: path.Join(append([]string{"/dav/files", scope}, segs...)...),
		scope: scope, segs: segs,
		teamID: s.Team.ID, ownerID: s.User.ID,
		folderID: folderID,
		wbuf:     tmp,
		created:  !found,
	}
	if found {
		f.fileID = file.ID
		f.teamID = file.TeamID
		f.ownerID = file.OwnerID
	}
	f.info = xfileInfo{
		name: leaf, mode: 0o644, modTime: time.Now(),
		etag: fileEtag(f.fileID, time.Now().String(), 0),
	}
	return f, nil
}

// fileMetaFile returns a File that only carries metadata + dead props
// (used by PROPPATCH on an existing file).
func (w *filesWebDAV) fileMetaFile(ctx context.Context, scope string, segs []string, file *models.File, name string) *xfile {
	return &xfile{
		w: w, ctx: ctx, name: name, scope: scope, segs: segs,
		fileID: file.ID, teamID: file.TeamID, ownerID: file.OwnerID,
		folderID: file.FolderID,
		info: xfileInfo{
			name: segs[len(segs)-1], size: file.Size, mode: 0o644,
			modTime: file.UpdatedAt, etag: fileEtag(file.ID, file.UpdatedAt.String(), file.Size),
		},
	}
}

// folderMetaFile returns a File that only carries metadata + dead props
// (used by PROPPATCH on an existing folder).
func (w *filesWebDAV) folderMetaFile(ctx context.Context, scope string, segs []string, folder *models.FileFolder, name string) *xfile {
	return &xfile{
		w: w, ctx: ctx, name: name, scope: scope, segs: segs,
		isDir: true, folderID: folder.ID,
		info: xfileInfo{
			name: segs[len(segs)-1], mode: os.ModeDir | 0o755,
			modTime: folder.UpdatedAt, etag: fileEtag(folder.ID, folder.UpdatedAt.String(), 0),
		},
	}
}

// --- webdav.File ---

func (f *xfile) Close() error {
	if f.wbuf != nil {
		if _, err := f.wbuf.Seek(0, io.SeekStart); err != nil {
			return err
		}
		key := contentKey(f.teamID, f.scope, f.fileID)
		if !f.created {
			key = contentKey(f.teamID, f.scope, f.fileID)
		}
		if f.fileID == "" {
			f.fileID = uuid.Must(uuid.NewV7()).String()
			key = contentKey(f.teamID, f.scope, f.fileID)
		}
		if err := f.w.h.st.Save(f.ctx, key, f.wbuf); err != nil {
			return err
		}
		stat, err := f.wbuf.Stat()
		if err != nil {
			return err
		}
		leaf := f.segs[len(f.segs)-1]
		ctype := mime.TypeByExtension(path.Ext(leaf))
		now := time.Now()
		db, err := f.w.db(f.ctx)
		if err != nil {
			return err
		}
		if f.created {
			rec := models.File{
				ID:       f.fileID,
				TeamID:   f.teamID,
				Scope:    f.scope,
				OwnerID:  f.ownerID,
				Name:     leaf,
				MimeType: ctype,
				Size:     stat.Size(),
				FolderID: f.folderID,
			}
			if err := db.Create(&rec).Error; err != nil {
				return err
			}
			f.info.etag = fileEtag(f.fileID, now.String(), stat.Size())
		} else {
			if err := db.Model(&models.File{}).Where("id = ?", f.fileID).Updates(map[string]any{
				"size": stat.Size(), "mime_type": ctype, "updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		f.info.size = stat.Size()
		f.info.modTime = now
	}
	if f.spool != nil {
		f.spool.Close()
		os.Remove(f.spool.Name())
	}
	if f.rcloser != nil {
		f.rcloser.Close()
	}
	if f.wbuf != nil {
		f.wbuf.Close()
		os.Remove(f.wbuf.Name())
	}
	return nil
}

func (f *xfile) Read(p []byte) (int, error) {
	if f.rs == nil {
		return 0, fs.ErrInvalid
	}
	return f.rs.Read(p)
}

func (f *xfile) Seek(offset int64, whence int) (int64, error) {
	if f.rs == nil {
		return 0, fs.ErrInvalid
	}
	return f.rs.Seek(offset, whence)
}

func (f *xfile) Write(p []byte) (int, error) {
	if f.wbuf == nil {
		return 0, fs.ErrInvalid
	}
	n, err := f.wbuf.Write(p)
	f.wsize += int64(n)
	return n, err
}

func (f *xfile) Readdir(count int) ([]os.FileInfo, error) {
	if !f.isDir {
		return nil, fs.ErrInvalid
	}
	old := f.dirIdx
	if old >= len(f.children) {
		if count > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	if count > 0 {
		f.dirIdx += count
		if f.dirIdx > len(f.children) {
			f.dirIdx = len(f.children)
		}
	} else {
		f.dirIdx = len(f.children)
		old = 0
	}
	return f.children[old:f.dirIdx], nil
}

func (f *xfile) Stat() (os.FileInfo, error) {
	if f.wbuf != nil {
		now := time.Now()
		info := f.info
		info.size = f.wsize
		info.modTime = now
		if f.fileID != "" {
			info.etag = fileEtag(f.fileID, now.String(), f.wsize)
		}
		return info, nil
	}
	return f.info, nil
}

// --- DeadPropsHolder (PROPPATCH support) ---

func (f *xfile) DeadProps() (map[xml.Name]webdav.Property, error) {
	raw, err := f.loadDeadProps()
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func (f *xfile) Patch(patches []webdav.Proppatch) ([]webdav.Propstat, error) {
	props, err := f.loadDeadProps()
	if err != nil {
		return nil, err
	}
	pstat := webdav.Propstat{Status: http.StatusOK}
	for _, patch := range patches {
		for _, p := range patch.Props {
			pstat.Props = append(pstat.Props, webdav.Property{XMLName: p.XMLName})
			if patch.Remove {
				delete(props, p.XMLName)
				continue
			}
			if props == nil {
				props = map[xml.Name]webdav.Property{}
			}
			props[p.XMLName] = p
		}
	}
	if err := f.saveDeadProps(props); err != nil {
		return nil, err
	}
	return []webdav.Propstat{pstat}, nil
}

func (f *xfile) loadDeadProps() (map[xml.Name]webdav.Property, error) {
	db, err := f.w.db(f.ctx)
	if err != nil {
		return nil, err
	}
	var raw string
	if f.fileID != "" {
		var rec models.File
		if err := db.Select("dead_props").Where("id = ?", f.fileID).First(&rec).Error; err != nil {
			return nil, err
		}
		raw = rec.DeadProps
	} else if f.folderID != "" {
		var rec models.FileFolder
		if err := db.Select("dead_props").Where("id = ?", f.folderID).First(&rec).Error; err != nil {
			return nil, err
		}
		raw = rec.DeadProps
	}
	if raw == "" {
		return map[xml.Name]webdav.Property{}, nil
	}
	var list []webdav.Property
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, err
	}
	out := make(map[xml.Name]webdav.Property, len(list))
	for _, p := range list {
		out[p.XMLName] = p
	}
	return out, nil
}

func (f *xfile) saveDeadProps(props map[xml.Name]webdav.Property) error {
	db, err := f.w.db(f.ctx)
	if err != nil {
		return err
	}
	list := make([]webdav.Property, 0, len(props))
	for _, p := range props {
		list = append(list, p)
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if f.fileID != "" {
		return db.Model(&models.File{}).Where("id = ?", f.fileID).Update("dead_props", string(data)).Error
	}
	if f.folderID != "" {
		return db.Model(&models.FileFolder{}).Where("id = ?", f.folderID).Update("dead_props", string(data)).Error
	}
	return nil
}
