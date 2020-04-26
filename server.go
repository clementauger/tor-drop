package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/andrewstuart/limio"
	"github.com/k1LoW/duration"

	humanize "github.com/dustin/go-humanize"
	"github.com/miolini/datacounter"
)

type torDropFileServer struct {
	logger *logWriter

	db   *torDropDB
	conf torDropConfig

	UpdateInterval   time.Duration
	AutosaveInterval time.Duration

	DataFile string

	ops          chan func()
	uploadEvents chan fileUpload
	freeSlot     chan bool

	folderUploadManagers   map[string]*folderManager
	folderDownloadManagers map[string]*folderManager
	activeDownloads        map[string]int
}

func newFileServer(conf torDropConfig) *torDropFileServer {
	return &torDropFileServer{
		conf:   conf,
		logger: newLogger("file-server"),
		db: &torDropDB{
			Items: map[string]fileItems{},
		},
		AutosaveInterval: time.Minute,
		UpdateInterval:   time.Minute,
		ops:              make(chan func()),
		uploadEvents:     make(chan fileUpload),
		freeSlot:         make(chan bool),
		DataFile:         "db.json",
	}
}

type bytesDecoder uint64

func (e *bytesDecoder) UnmarshalText(text []byte) error {
	if len(text) < 1 {
		return nil
	}
	g, err := humanize.ParseBytes(string(text))
	if err == nil {
		*e = bytesDecoder(g)
	}
	return err
}

func (e *bytesDecoder) UnmarshalJSON(text []byte) error {
	x, err := strconv.ParseUint(string(text), 10, 64)
	if err == nil {
		*e = bytesDecoder(x)
	}
	return err
}

type durationDecoder time.Duration

func (e *durationDecoder) UnmarshalText(text []byte) error {
	if len(text) < 1 {
		return nil
	}
	g, err := duration.Parse(string(text))
	if err == nil {
		*e = durationDecoder(g)
	}
	return err
}

func (e *durationDecoder) UnmarshalJSON(text []byte) error {
	u, err := strconv.ParseInt(string(text), 10, 64)
	if err != nil {
		return err
	}
	*e = durationDecoder(u)
	return err
}

type folder struct {
	Name                  string
	CreateDate            time.Time
	MaxFileSize           *bytesDecoder
	MaxFileCount          *uint64
	MaxTotalSize          *bytesDecoder
	MaxLifeTime           *durationDecoder
	MaxUpBytesPerSec      *bytesDecoder
	MaxDlBytesPerSec      *bytesDecoder
	MaxActiveUploads      *int
	MaxActiveDownloads    *int
	CaptchaForAnonymous   bool
	CaptchaForLoggedUsers bool
	IsPrivate             bool
	IsAdminOnlyReadable   bool
	Password              *string
	Users                 map[string][]string
}

type fileItem struct {
	Name       string
	Path       string
	CreateDate time.Time
	Size       uint64
	Uploaded   uint64
}

func (f fileItem) IsComplete() bool {
	return f.Uploaded >= f.Size
}

type fileUpload struct {
	TmpFile    string
	Folder     string
	File       fileItem
	LastActive time.Time
	Error      error
	Completed  chan error
}

type torDropDB struct {
	Uploads fileUploads `json:"-"`
	Folders folders
	Items   map[string]fileItems
}

func (t *torDropFileServer) load() error {
	f, err := os.Open(t.DataFile)
	if err != nil {
		return err
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&t.db)
	return err
}

func (t *torDropFileServer) save() error {
	f, err := os.OpenFile(t.DataFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", " ")
	err = enc.Encode(t.db)
	return err
}

type folderManager struct {
	*limio.SimpleManager
	n int
}

func (t *torDropFileServer) setUploadLimit(folderName string, n int) error {
	x, ok := t.folderUploadManagers[folderName]
	if ok {
		if x.n == n {
			return nil
		}
	}
	if n == 0 {
		if x != nil {
			x.Close()
		}
		delete(t.folderUploadManagers, folderName)
		return nil
	}
	if !ok {
		t.folderUploadManagers[folderName] = &folderManager{
			SimpleManager: limio.NewSimpleManager(),
			n:             n,
		}
	}
	t.folderUploadManagers[folderName].n = n
	t.folderUploadManagers[folderName].SimpleLimit(n, time.Second)
	return nil
}

func (t *torDropFileServer) setDownloadLimit(folderName string, n int) error {
	x, ok := t.folderDownloadManagers[folderName]
	if ok {
		if x.n == n {
			return nil
		}
	}
	if n == 0 {
		if x != nil {
			x.Close()
		}
		delete(t.folderDownloadManagers, folderName)
		return nil
	}
	if !ok {
		t.folderDownloadManagers[folderName] = &folderManager{
			SimpleManager: limio.NewSimpleManager(),
			n:             n,
		}
	}
	t.folderDownloadManagers[folderName].n = n
	t.folderDownloadManagers[folderName].SimpleLimit(n, time.Second)
	return nil
}

func (t *torDropFileServer) getUploadReader(folderName string, src io.ReadCloser) io.ReadCloser {
	x, ok := t.folderUploadManagers[folderName]
	if !ok {
		return src
	}
	return readCloser{Closer: src, Reader: x.NewReader(src)}
}

func (t *torDropFileServer) getDownloadReader(folderName string, src io.ReadCloser) io.ReadCloser {
	x, ok := t.folderDownloadManagers[folderName]
	if !ok {
		return src
	}
	return readCloser{Closer: src, Reader: x.NewReader(src)}
}

func (t *torDropFileServer) Listen(ctx context.Context) error {
	if t.activeDownloads == nil {
		t.activeDownloads = map[string]int{}
	}
	if t.folderUploadManagers == nil {
		t.folderUploadManagers = map[string]*folderManager{}
	}
	if t.folderDownloadManagers == nil {
		t.folderDownloadManagers = map[string]*folderManager{}
	}

	t.logger.Info("starting tor-drop file server...")

	if t.UpdateInterval < 1 {
		t.UpdateInterval = time.Minute
	}
	if t.AutosaveInterval < 1 {
		t.AutosaveInterval = time.Minute * 2
	}

	if err := t.load(); err != nil {
		t.logger.Info("failed to load tor-drop database: %v", err)
	}

	t.db.ClearUploads(func(up fileUpload) {
		err := os.Remove(up.TmpFile)
		t.logger.Info("removed tmp file %q err=%v", up.TmpFile, err)
	})

	if err := t.save(); err != nil {
		t.logger.Info("failed to save tor-drop database: %v", err)
	}
	defer func() {
		err := t.save()
		t.logger.Info("saving tor-drop database: %v", err)
	}()

	tm := time.NewTicker(t.UpdateInterval)
	defer tm.Stop()
	t2m := time.NewTicker(t.AutosaveInterval)
	defer t2m.Stop()

	for _, folder := range t.db.GetFolders() {
		if folder.MaxDlBytesPerSec != nil && *folder.MaxDlBytesPerSec > 0 {
			t.setDownloadLimit(folder.Name, int(*folder.MaxDlBytesPerSec))
		}
		if folder.MaxUpBytesPerSec != nil && *folder.MaxUpBytesPerSec > 0 {
			t.setUploadLimit(folder.Name, int(*folder.MaxUpBytesPerSec))
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t2m.C:
			if err := t.save(); err != nil {
				t.logger.Info("failed to save tor-drop database: %v", err)
				continue
			}
			t.logger.Info("saved tor-drop database to %v", t.DataFile)

		case <-tm.C:
			t.logger.Info("checking tor-drop uploads...")

			lifetime := time.Minute * 15
			t.db.ClearLifetimeExceededUploads(lifetime, func(up fileUpload) {
				t.logger.Info("detected inactive file upload %q since %v", up.File.Name,
					time.Now().Add(lifetime).Sub(up.LastActive))
				go func() {
					up.Completed <- fmt.Errorf("cancelled because too slow")
				}()
				err := os.Remove(up.TmpFile)
				if err != nil {
					t.logger.Info("failed to remove temp file %q err=%v", up.TmpFile, err)
				}
			})

			t.db.ClearLifetimeExceededItems(func(folderName string, i fileItem) {
				t.logger.Info("max lifetime exceeded for file %v/%v", folderName, i.Name)
				u := filepath.Join(t.conf.StorageDir, folderName, i.Name)
				err := os.Remove(u)
				if err != nil {
					t.logger.Error("failed to delete file for lifetime exceeded: %v", folderName, i.Name, err)
				}
			})

			t.save()

		case ev := <-t.uploadEvents:
			if ev.File.IsComplete() {
				select {
				case t.freeSlot <- true:
				default:
				}
				t.db.CompleteUpload(ev)
				if ev.Error != nil {
					ev.Completed <- ev.Error
					t.logger.Error("file %q upload completion error: %v", ev.File.Name, ev.Error)
					continue
				}

				_, err := t.db.GetItem(ev.Folder, ev.File.Name)
				if err == nil {
					err := fmt.Errorf("file %q upload completion error: %v", ev.File.Name, fmt.Errorf("file %q already exists", ev.File.Name))
					t.logger.Error("%v", err)
					ev.Completed <- err
					err = os.Remove(ev.TmpFile)
					if err != nil {
						t.logger.Error("temporary file %q upload completion error: %v", ev.File.Name, err)
					}
					continue
				}

				du := filepath.Join(t.conf.StorageDir, ev.Folder)
				t.logger.Info("creating path %q: %v", du, os.MkdirAll(du, os.ModePerm))

				u := filepath.Join(t.conf.StorageDir, ev.Folder, ev.File.Name)
				if err := os.Rename(ev.TmpFile, u); err != nil {
					t.logger.Error("file %q upload completion error: %v", ev.File.Name, err)
					ev.Completed <- err
					err = os.Remove(ev.TmpFile)
					t.logger.Error("temporary file %q upload completion error: %v", ev.File.Name, err)
					continue
				}
				if err := t.db.AddItem(ev.Folder, ev.File); err != nil {
					t.logger.Error("file %q upload completion error: %v", ev.File.Name, err)
					ev.Completed <- err
					err = os.Remove(u)
					if err != nil {
						t.logger.Error("file %q upload cleaning error: %v", ev.File.Name, err)
					}
					continue
				}
				t.logger.Printf("added file %q to %q\n", ev.File.Name, u)
				if err := t.save(); err != nil {
					ev.Completed <- err
					t.logger.Error("failed to save the database: %v", err)
					continue
				}
				ev.Completed <- nil
				continue
			}
			if !t.db.UploadEventNewer(ev) {
				log.Printf("ev ent not updated %v\n", ev)
			}

		case op := <-t.ops:
			op()
		}
	}
}

func (t *torDropFileServer) Folders(includePrivate bool) []folder {
	ret := make(chan []folder)
	t.ops <- func() {
		fds := t.db.GetFolders()
		if !includePrivate {
			fds = fds.Public()
		}
		ret <- fds
	}
	return <-ret
}

func (t *torDropFileServer) Folder(name string) *folder {
	ret := make(chan *folder)
	t.ops <- func() {
		ret <- t.db.Folder(name)
	}
	return <-ret
}

func (t *torDropFileServer) UpdateFolder(fd folder, users bool) error {
	ret := make(chan error)
	t.ops <- func() {
		err := t.db.UpdateFolder(fd, users)
		if err == nil {
			if fd.MaxDlBytesPerSec != nil {
				t.setDownloadLimit(fd.Name, int(*fd.MaxDlBytesPerSec))
			}
			if fd.MaxUpBytesPerSec != nil {
				t.setUploadLimit(fd.Name, int(*fd.MaxUpBytesPerSec))
			}
		}
		if err == nil {
			err = t.save()
		}
		ret <- err
	}
	return <-ret
}

func (t *torDropFileServer) Items(folderName string, uploading bool) (fileItems, error) {
	var cp []fileItem
	ret := make(chan error)
	t.ops <- func() {
		x, err := t.db.GetItems(folderName, uploading)
		cp = append(cp, x...)
		ret <- err
	}
	return cp, <-ret
}

func (t *torDropFileServer) Item(folderName string, name string) (fileItem, error) {
	ret := make(chan error)
	var f fileItem
	t.ops <- func() {
		x, err := t.db.GetItem(folderName, name)
		f = x
		ret <- err
	}
	return f, <-ret
}

func (t *torDropFileServer) RmFolder(name string) error {
	ret := make(chan error)
	t.ops <- func() {
		err := t.db.RmFolder(name)
		if err == nil {
			x, ok := t.folderUploadManagers[name]
			if ok {
				x.Close()
				delete(t.folderUploadManagers, name)
			}
			x, ok = t.folderDownloadManagers[name]
			if ok {
				x.Close()
				delete(t.folderDownloadManagers, name)
			}
		}
		if err == nil {
			err = t.fsRemoveFolder(name)
		}
		if err == nil {
			err = t.save()
		}
		ret <- err
	}
	return <-ret
}

func (t *torDropFileServer) RmItem(folderName, name string) error {
	ret := make(chan error)
	t.ops <- func() {
		var err error
		err = t.db.RmItem(folderName, name)
		if err == nil {
			err = t.fsRemove(folderName, name)
		}
		if err == nil {
			err = t.save()
		}
		ret <- err
	}
	return <-ret
}

func (t *torDropFileServer) fsRemoveFolder(name string) error {
	fsPath := filepath.Join(t.conf.StorageDir, name)
	return os.RemoveAll(fsPath)
}
func (t *torDropFileServer) fsRemove(folder, name string) error {
	fsPath := filepath.Join(t.conf.StorageDir, folder, name)
	return os.Remove(fsPath)
}

func (t *torDropFileServer) CreateFolder(fd folder) error {
	ret := make(chan error)
	t.ops <- func() {
		var err error
		err = t.db.CreateFolder(fd)
		if err == nil {
			if fd.MaxDlBytesPerSec != nil {
				t.setDownloadLimit(fd.Name, int(*fd.MaxDlBytesPerSec))
			}
			if fd.MaxUpBytesPerSec != nil {
				t.setUploadLimit(fd.Name, int(*fd.MaxUpBytesPerSec))
			}
		}
		if err == nil {
			err = t.save()
		}
		ret <- err
	}
	return <-ret
}

func (t *torDropFileServer) AddFolderLogin(folderName, user, pwd string) error {
	if folderName == "" {
		return fmt.Errorf("folder name must not be empty")
	}
	if user == "" {
		return fmt.Errorf("user name must not be empty")
	}
	ret := make(chan error)
	t.ops <- func() {
		var err error
		fd := t.db.Folder(folderName)
		if fd == nil {
			ret <- fmt.Errorf("folder %q not found", folderName)
			return
		}
		if fd.Users == nil {
			fd.Users = map[string][]string{}
		}
		_, ok := fd.Users[user]
		if !ok {
			fd.Users[user] = []string{pwd}
			err = t.db.UpdateFolder(*fd, true)
			if err == nil {
				err = t.save()
			}
		}
		ret <- err
	}
	return <-ret
}

func (t *torDropFileServer) RmFolderLogin(folderName, user string) error {
	ret := make(chan error)
	t.ops <- func() {
		var err error
		fd := t.db.Folder(folderName)
		if fd == nil {
			ret <- fmt.Errorf("folder %q not found", folderName)
			return
		}
		if fd.Users != nil {
			delete(fd.Users, user)
			if len(fd.Users) < 1 {
				fd.Users = nil
			}
		}
		if err == nil {
			err = t.save()
		}
		ret <- err
	}
	return <-ret
}

func (t *torDropFileServer) WriteItem(folderName string, name string, content []byte) error {
	if folderName == "" {
		return fmt.Errorf("folder name must not be empty")
	}
	if name == "" {
		return fmt.Errorf("file name must not be empty")
	}
	ret := make(chan error)
	t.ops <- func() {

		item, err := t.db.GetItem(folderName, name)
		if err != nil {
			ret <- err
			return
		}
		if t.db.HasUpload(folderName, name) {
			ret <- fmt.Errorf("file %q already being uploaded", item.Name)
			return
		}

		fd := t.db.Folder(folderName)
		items, err := t.db.GetItems(folderName, true)
		if err != nil {
			ret <- err
			return
		}

		fsize := uint64(len(content))
		if fd.MaxFileSize != nil {
			mfs := uint64(*fd.MaxFileSize)
			if fsize > mfs {
				ret <- fmt.Errorf("the file too large, must not exceed %v", humanize.Bytes(mfs))
				return
			}
		}
		if fd.MaxFileCount != nil {
			mfc := *fd.MaxFileCount
			if uint64(len(items)+1) > mfc {
				ret <- fmt.Errorf("this folder cannot accept more files")
				return
			}
		}
		if fd.MaxTotalSize != nil {
			mts := uint64(*fd.MaxTotalSize)
			curSize := items.Size()
			if curSize+fsize > mts {
				ret <- fmt.Errorf("file is too large, demands %v, only %v available", humanize.Bytes(fsize), humanize.Bytes(mts-curSize))
				return
			}
		}

		fpath := filepath.Join(t.conf.StorageDir, folderName, item.Name)
		os.Remove(fpath)
		err = ioutil.WriteFile(fpath, content, os.ModePerm)
		if err != nil {
			os.Remove(fpath)
			ret <- err
			return
		}

		item.Size = fsize
		item.CreateDate = time.Now()

		ret <- t.db.AddItem(folderName, item)
	}
	return <-ret
}

func (t *torDropFileServer) OpenItem(folderName string, fileName string) (io.ReadCloser, error) {
	if folderName == "" {
		return nil, fmt.Errorf("folder name must not be empty")
	}
	if fileName == "" {
		return nil, fmt.Errorf("file name must not be empty")
	}
	var src io.ReadCloser
	ret := make(chan error)
	t.ops <- func() {
		item, err := t.db.GetItem(folderName, fileName)
		if err != nil {
			ret <- err
			return
		}

		fd := t.db.Folder(folderName)
		if fd == nil {
			ret <- fmt.Errorf("folder %q does not exist", folderName)
			return
		}

		if fd.MaxActiveUploads != nil &&
			*fd.MaxActiveUploads > 0 {
			x := t.activeDownloads[fd.Name]
			if x >= *fd.MaxActiveUploads {
				ret <- fmt.Errorf("maximum active uploads exceeded, try again later")
				return
			}
		}
		t.activeDownloads[fd.Name]++

		fpath := filepath.Join(t.conf.StorageDir, folderName, item.Name)
		src, err = os.Open(fpath)
		src = t.getUploadReader(folderName, src)
		src = &readDownloader{
			ReadCloser: src,
			fd:         fd.Name,
			fs:         t,
		}
		ret <- err
	}
	return src, <-ret
}

type readDownloader struct {
	io.ReadCloser
	fd string
	fs *torDropFileServer
}

func (r *readDownloader) Close() error {
	r.fs.ops <- func() {
		if r.fs.activeDownloads[r.fd] > 0 {
			r.fs.activeDownloads[r.fd]--
		}
	}
	return r.ReadCloser.Close()
}

type readCloser struct {
	io.Closer
	io.Reader
}

func (t *torDropFileServer) UploadItem(folderName string, item fileItem, src io.ReadCloser) error {
	if folderName == "" {
		return fmt.Errorf("folder name must not be empty")
	}
	if item.Name == "" {
		return fmt.Errorf("file name must not be empty")
	}
	if item.Size < 1 {
		return fmt.Errorf("content length must be greater than zero")
	}
	ret := make(chan error)
	t.ops <- func() {

		fd := t.db.Folder(folderName)
		if fd == nil {
			ret <- fmt.Errorf("folder %q does not exist", folderName)
			return
		}

		if fd.MaxActiveDownloads != nil &&
			*fd.MaxActiveDownloads > 0 &&
			t.db.UploadCount(fd.Name) >= *fd.MaxActiveDownloads {
			ret <- fmt.Errorf("maximum active downloads exceeded, try again later")
			return
		}

		items, err := t.db.GetItems(folderName, true)
		if err != nil {
			ret <- err
			return
		}
		if items.Has(item.Name) {
			ret <- fmt.Errorf("file %q already exists or being uploaded", item.Name)
			return
		}

		fsize := item.Size
		if fd.MaxFileSize != nil {
			mfs := uint64(*fd.MaxFileSize)
			if fsize > mfs && mfs > 0 {
				ret <- fmt.Errorf("the file too large, must not exceed %v", humanize.Bytes(mfs))
				return
			}
		}
		if fd.MaxFileCount != nil {
			mfc := *fd.MaxFileCount
			if uint64(len(items)+1) > mfc && mfc > 0 {
				ret <- fmt.Errorf("this folder cannot accept more files")
				return
			}
		}
		if fd.MaxTotalSize != nil {
			mts := uint64(*fd.MaxTotalSize)
			curSize := items.Size()
			if curSize+fsize > mts && mts > 0 {
				ret <- fmt.Errorf("file is too large, demands %v, only %v available", humanize.Bytes(fsize), humanize.Bytes(mts-curSize))
				return
			}
		}

		src = t.getDownloadReader(fd.Name, src)

		var up fileUpload
		up.Completed = make(chan error)
		up.File = item
		up.Folder = folderName
		up.LastActive = time.Now()
		t.db.Uploads = append(t.db.Uploads, up)

		go func() {
			tfile, _ := ioutil.TempFile(t.conf.TmpDir, "tor-drop")
			defer tfile.Close()
			up.TmpFile = tfile.Name()
			dc := datacounter.NewWriterCounter(tfile)
			errC := make(chan error)
			go func() {
				defer src.Close()
				_, err := io.Copy(dc, src)
				errC <- err
			}()
			var d bool
			for !d {
				select {
				case <-time.After(time.Second):
					up.LastActive = time.Now()
					up.File.Uploaded = dc.Count()
					t.uploadEvents <- up
				case err := <-up.Completed:
					ret <- err
					d = true
				case err := <-errC:
					up.LastActive = time.Now()
					up.File.Uploaded = dc.Count()
					up.Error = err
					t.uploadEvents <- up
				}
			}
		}()
	}
	return <-ret
}

func (t *torDropDB) HasUpload(folderName string, name string) bool {
	return t.Uploads.Has(folderName, name)
}

func (t *torDropDB) GetFolders() folders {
	var folders folders
	folders = append(folders, t.Folders...)
	return folders
}

func (t *torDropDB) ClearUploads(each func(fileUpload)) {
	for _, up := range t.Uploads {
		each(up)
	}
	t.Uploads = fileUploads{}
}

func (t *torDropDB) ClearLifetimeExceededUploads(lifetime time.Duration, exceeded func(fileUpload)) {
	var n fileUploads
	for _, up := range t.Uploads {
		if up.LastActive.Add(lifetime).Before(time.Now()) {
			exceeded(up)
			continue
		}
		n = append(n, up)
	}
	t.Uploads = n
}

func (t *torDropDB) ClearLifetimeExceededItems(exceeded func(string, fileItem)) {
	for folderName, items := range t.Items {
		var f fileItems
		fd := t.Folder(folderName)
		if fd == nil {
			continue
		}
		if fd.MaxLifeTime == nil {
			continue
		}
		lm := time.Duration(*fd.MaxLifeTime)
		for _, i := range items {
			if time.Now().Sub(i.CreateDate) > lm {
				exceeded(folderName, i)
				continue
			}
			f = append(f, i)
		}
		t.Items[folderName] = f
	}
}

func (t *torDropDB) CompleteUpload(ev fileUpload) {
	t.Uploads = t.Uploads.Remove(ev)
}
func (t *torDropDB) UploadEvent(ev fileUpload) bool {
	return t.Uploads.Update(ev)
}
func (t *torDropDB) UploadEventNewer(ev fileUpload) bool {
	return t.Uploads.UpdateNewer(ev)
}

func (t *torDropDB) UploadCount(folderName string) int {
	if folderName == "" {
		return len(t.Uploads)
	}
	return len(t.Uploads.Items(folderName))
}

func (t *torDropDB) Folder(name string) *folder {
	if t.Folders.Has(name) {
		cp := t.Folders.Get(name)
		return &cp
	}
	return nil
}

func (t *torDropDB) UpdateFolder(fd folder, users bool) error {
	x := t.Folder(fd.Name)
	if x == nil {
		return fmt.Errorf("failed to update folder %q, it has gone away", fd.Name)
	}
	if !users {
		fd.Users = x.Users
	}
	fd.CreateDate = x.CreateDate
	t.Folders.Set(fd)
	return nil
}

func (t *torDropDB) GetItems(folderName string, uploading bool) (fileItems, error) {
	var cp []fileItem
	x, ok := t.Items[folderName]
	if !ok {
		return cp, nil
	}
	cp = append(cp, x...)
	if uploading {
		cp = append(cp, t.Uploads.Items(folderName)...)
	}
	return cp, nil
}

func (t *torDropDB) GetItem(folderName string, name string) (fileItem, error) {
	var f fileItem
	items, ok := t.Items[folderName]
	if !ok {
		return f, fmt.Errorf("folder %q not found", folderName)
	}
	if !items.Has(name) {
		return f, fmt.Errorf("file %q not found in folder %q", name, folderName)
	}
	f = items.Get(name)
	return f, nil
}

func (t *torDropDB) RmFolder(name string) error {
	if !t.Folders.Has(name) {
		return fmt.Errorf("folder %q not found", name)
	}
	var n []folder
	for _, f := range t.Folders {
		if f.Name == name {
			continue
		}
		n = append(n, f)
	}
	delete(t.Items, name)
	t.Folders = n
	return nil
}

func (t *torDropDB) RmItem(folderName, name string) error {
	items, ok := t.Items[folderName]
	if !ok {
		return fmt.Errorf("folder %q not found", folderName)
	}
	if !items.Has(name) {
		return fmt.Errorf("file %q not found in folder %q", name, folderName)
	}
	var n []fileItem
	for _, i := range t.Items[folderName] {
		if i.Name == name {
			continue
		}
		n = append(n, i)
	}
	t.Items[folderName] = n
	return nil
}

func (t *torDropDB) clean(s string) string {
	if s == "" {
		return s
	}
	s = filepath.Clean(s)
	s = strings.Split(s, "/")[0]
	s = strings.Split(s, "\\")[0]
	return s
}

func (t *torDropDB) AddItem(folderName string, item fileItem) error {
	if folderName == "" {
		return fmt.Errorf("folder name must not be empty")
	}
	if t.Folder(folderName) == nil {
		return fmt.Errorf("folder %q does not exist", folderName)
	}
	if item.Name == "" {
		return fmt.Errorf("item name must not be empty")
	}
	items, _ := t.Items[folderName]
	items = append(items, item)
	t.Items[folderName] = items
	return nil
}

func (t *torDropDB) CreateFolder(fd folder) error {
	fd.Name = t.clean(fd.Name)
	if fd.Name == "" {
		return fmt.Errorf("folder name must not be empty")
	}
	if t.Folders.Has(fd.Name) {
		return fmt.Errorf("folder %q already exists", fd.Name)
	}
	fd.CreateDate = time.Now()
	t.Folders = append(t.Folders, fd)
	return nil
}

type fileUploads []fileUpload

func (f fileUploads) Remove(up fileUpload) (n fileUploads) {
	for _, fd := range f {
		if fd.Folder == up.Folder && fd.File.Name == up.File.Name {
			continue
		}
		n = append(n, fd)
	}
	return
}

func (f fileUploads) Has(folderName, name string) bool {
	for _, fd := range f {
		if fd.Folder == folderName && fd.File.Name == name {
			return true
		}
	}
	return false
}

func (f fileUploads) Get(folderName, name string) fileUpload {
	for _, fd := range f {
		if fd.Folder == folderName && fd.File.Name == name {
			return fd
		}
	}
	return fileUpload{}
}

func (f fileUploads) Update(up fileUpload) bool {
	for i, fd := range f {
		if fd.Folder == up.Folder && fd.File.Name == up.File.Name {
			f[i] = up
			return true
		}
	}
	return false
}

func (f fileUploads) UpdateNewer(up fileUpload) bool {
	for i, fd := range f {
		if fd.Folder == up.Folder && fd.File.Name == up.File.Name && fd.LastActive.Before(up.LastActive) {
			f[i] = up
			return true
		}
	}
	return false
}

func (f fileUploads) Items(folderName string) fileItems {
	var c fileItems
	for _, fd := range f {
		if fd.Folder == folderName {
			c = append(c, fd.File)
		}
	}
	return c
}

func (f fileUploads) Size(folderName string) uint64 {
	var c uint64
	for _, fd := range f {
		if fd.Folder == folderName {
			c += fd.File.Size
		}
	}
	return c
}

type folders []folder

func (f folders) Public() folders {
	var n folders
	for _, fd := range f {
		if fd.IsPrivate {
			continue
		}
		n = append(n, fd)
	}
	return n
}
func (f folders) Private() folders {
	var n folders
	for _, fd := range f {
		if !fd.IsPrivate {
			continue
		}
		n = append(n, fd)
	}
	return n
}

func (f folders) Has(name string) bool {
	for _, fd := range f {
		if fd.Name == name {
			return true
		}
	}
	return false
}

func (f folders) Get(name string) folder {
	for _, fd := range f {
		if fd.Name == name {
			return fd
		}
	}
	return folder{}
}

func (f folders) Set(fd folder) {
	for i, ffd := range f {
		if fd.Name == ffd.Name {
			f[i] = fd
			return
		}
	}
}

type fileItems []fileItem

func (f fileItems) Has(name string) bool {
	for _, fd := range f {
		if fd.Name == name {
			return true
		}
	}
	return false
}

func (f fileItems) Get(name string) fileItem {
	for _, fd := range f {
		if fd.Name == name {
			return fd
		}
	}
	return fileItem{}
}

func (f fileItems) Size() (curSize uint64) {
	for _, i := range f {
		curSize += i.Size
	}
	return curSize
}
