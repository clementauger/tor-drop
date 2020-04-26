package main

import (
	"encoding/gob"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"time"

	"github.com/davidbanham/human_duration"
	"github.com/dchest/captcha"
	humanize "github.com/dustin/go-humanize"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/gorilla/schema"
	"github.com/gorilla/sessions"
	"github.com/k1LoW/duration"
)

func getApps(secCookie string, fs *torDropFileServer, assetsDir string, static bool, captchaSolution string) (admin *mux.Router, public *mux.Router, err error) {

	public = mux.NewRouter()
	admin = mux.NewRouter()
	dec := schema.NewDecoder()
	dec.ZeroEmpty(false)
	dec.IgnoreUnknownKeys(true)
	store := sessions.NewCookieStore([]byte(secCookie))
	pubApp := &torDropApp{
		logger:          newLogger("app"),
		isAdmin:         false,
		fs:              fs,
		decoder:         dec,
		session:         store,
		captchaSolution: captchaSolution,
		assetsDir:       assetsDir,
		static:          static,
	}
	adminApp := &torDropApp{
		logger:    newLogger("app"),
		isAdmin:   true,
		fs:        fs,
		decoder:   dec,
		session:   store,
		assetsDir: assetsDir,
		static:    static,
	}
	funcs := map[string]interface{}{
		"csrf": csrf.TemplateField,
		"ints": func(u interface{}) string {
			switch x := u.(type) {
			case *int:
				if x == nil {
					return ""
				}
				return fmt.Sprint(*x)
			case int:
				return fmt.Sprint(x)
			case *uint64:
				if x == nil {
					return ""
				}
				return fmt.Sprint(*x)
			case uint64:
				return fmt.Sprint(x)
			}
			return ""
		},
		"bytes": func(u interface{}) string {
			switch x := u.(type) {
			case *bytesDecoder:
				if x == nil {
					return ""
				}
				return humanize.Bytes(uint64(*x))
			case bytesDecoder:
				return humanize.Bytes(uint64(x))
			case *uint64:
				if x == nil {
					return ""
				}
				return humanize.Bytes(*x)
			case uint64:
				return humanize.Bytes(x)
			}
			return ""
		},
		"since": func(u interface{}) string {
			switch x := u.(type) {
			case *time.Time:
				if x == nil {
					return ""
				}
				return humanize.Time(*x)
			case time.Time:
				return humanize.Time(x)
			}
			return ""
		},
		"times": func(u interface{}) string {
			switch x := u.(type) {
			case *time.Time:
				if x == nil {
					return ""
				}
				return x.Format("Mon Jan 02 15:04:05 2006")
			case time.Time:
				return x.Format("Mon Jan 02 15:04:05 2006")
			}
			return ""
		},
		"durations": func(u interface{}) string {
			switch x := u.(type) {
			case *durationDecoder:
				if x == nil {
					return ""
				}
				return human_duration.String(time.Duration(*x), "second")
			case durationDecoder:
				return human_duration.String(time.Duration(x), "second")
			case time.Duration:
				return human_duration.String(x, "second")
			case *time.Duration:
				if x == nil {
					return ""
				}
				return human_duration.String(*x, "second")
			case int64:
				return human_duration.String(time.Duration(x), "second")
			case *int64:
				if x == nil {
					return ""
				}
				return human_duration.String(time.Duration(*x), "second")
			}
			return ""
		},
		"isZero": func(u interface{}) bool {
			r := reflect.ValueOf(u)
			if r.Kind() == reflect.Ptr && r.IsNil() {
				return true
			}
			if r.Kind() == reflect.Ptr {
				r = r.Elem()
			}
			return r.Interface() == reflect.Zero(r.Type()).Interface()
		},
	}
	funcs["urlFor"] = func(s string, a ...string) string {
		u, err := public.GetRoute(s).URL(a...)
		if err != nil {
			return ""
		}
		return u.String()
	}
	err = pubApp.tpl.Build(funcs)
	if err != nil {
		return
	}
	funcs["urlFor"] = func(s string, a ...string) string {
		u, err := admin.GetRoute(s).URL(a...)
		if err != nil {
			return ""
		}
		return u.String()
	}
	err = adminApp.tpl.Build(funcs)
	if err != nil {
		return
	}
	public = pubApp.Mount(public)
	admin = adminApp.Mount(admin)
	return
}
func init() {
	gob.Register(userLogin{})
}

type torDropApp struct {
	router          *mux.Router
	session         sessions.Store
	logger          *logWriter
	isAdmin         bool
	fs              *torDropFileServer
	tpl             torDropTpl
	decoder         *schema.Decoder
	captchaSolution string
	assetsDir       string
	static          bool
}

type torDropTpl struct {
	index         tplExecer
	createFolder  tplExecer
	folderListing tplExecer
	folderLogin   tplExecer
	assetInfo     tplExecer
	// assetUpload   tplExecer
}

func (t *torDropTpl) Build(funcs template.FuncMap) (err error) {
	t.index, err = fileTemplate(funcs,
		"templates/index-custom.tpl", "templates/index.tpl",
		"templates/layout-custom.tpl", "templates/layout.tpl")
	t.createFolder, err = fileTemplate(funcs,
		"templates/create-folder-custom.tpl", "templates/create-folder.tpl",
		"templates/layout-custom.tpl", "templates/layout.tpl")
	t.folderListing, err = fileTemplate(funcs,
		"templates/folder-listing-custom.tpl", "templates/folder-listing.tpl",
		"templates/layout-custom.tpl", "templates/layout.tpl")
	t.folderLogin, err = fileTemplate(funcs,
		"templates/folder-login-custom.tpl", "templates/folder-login.tpl",
		"templates/layout-custom.tpl", "templates/layout.tpl")
	t.assetInfo, err = fileTemplate(funcs,
		"templates/asset-info-custom.tpl", "templates/asset-info.tpl",
		"templates/layout-custom.tpl", "templates/layout.tpl")
	// t.assetUpload, err = fileTemplate(funcs,
	// 	"templates/asset-upload-custom.tpl", "templates/asset-upload.tpl",
	// 	"templates/layout-custom.tpl", "templates/layout.tpl")
	return
}

func (t *torDropApp) Index(w http.ResponseWriter, r *http.Request) {
	folders := t.fs.Folders(t.isAdmin)
	data := map[string]interface{}{
		"IsAdmin": t.isAdmin,
		"Request": r,
		"Folders": folders,
		"Now":     time.Now(),
	}
	err := t.tpl.index.Execute(w, data)
	if err != nil {
		log.Printf("failed to serve index handler: %v\n", err)
	}
}

type userLogin struct {
	Login    string
	Password string
}
type folderCreate struct {
	Folder folder
	User   userLogin
}

func (t *torDropApp) CreateFolder(w http.ResponseWriter, r *http.Request) {
	var err error
	var fd folder
	var fc folderCreate
	if r.Method == http.MethodPost {
		err = r.ParseForm()
		if err == nil {
			if err = t.decoder.Decode(&fc, r.Form); err == nil {
				fd = fc.Folder
				err = t.fs.CreateFolder(fd)
				if err == nil {
					if fc.User.Login != "" {
						err = t.fs.AddFolderLogin(fd.Name, fc.User.Login, fc.User.Password)
					}
				}
				if err == nil {
					var url *url.URL
					url, err = t.router.Get("folder-edit").URL("folder", fd.Name)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					http.Redirect(w, r, url.String(), http.StatusSeeOther)
					return
				}
			}
		}
	} else {
		fd.CaptchaForAnonymous = true
		fd.CreateDate = time.Now()
		k := uint64(30)
		fd.MaxFileCount = &k
		l, _ := humanize.ParseBytes("10 mb")
		ll := bytesDecoder(l)
		fd.MaxFileSize = &ll
		l, _ = humanize.ParseBytes("200 kb")
		ll2 := bytesDecoder(l)
		fd.MaxUpBytesPerSec = &ll2
		l, _ = humanize.ParseBytes("100 kb")
		ll3 := bytesDecoder(l)
		fd.MaxDlBytesPerSec = &ll3
		j, _ := duration.Parse("7 days")
		jj := durationDecoder(j)
		fd.MaxLifeTime = &jj
		max := 5
		fd.MaxActiveUploads = &max
		fd.MaxActiveDownloads = &max
		fd.CaptchaForAnonymous = true
	}
	data := map[string]interface{}{
		"IsAdmin": t.isAdmin,
		"action":  "create",
		"Request": r,
		"Error":   err,
		"Folder":  fd,
		"Now":     time.Now(),
	}
	err = t.tpl.createFolder.Execute(w, data)
	if err != nil {
		log.Printf("failed to serve create-folder handler: %v\n", err)
	}
}
func (t *torDropApp) EditFolder(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)
	folderName := vars["folder"]
	var fd folder
	var fc folderCreate
	if r.Method == http.MethodPost {
		err = r.ParseForm()
		if err == nil {
			if r.Form.Get("action") == "rm-user" {
				err = t.fs.RmFolderLogin(folderName, r.Form.Get("User"))
			} else {
				if err = t.decoder.Decode(&fc, r.Form); err == nil {
					fd = fc.Folder
					err = t.fs.UpdateFolder(fd, false)
					if err == nil {
						if fc.User.Login != "" {
							err = t.fs.AddFolderLogin(fd.Name, fc.User.Login, fc.User.Password)
						}
					}
					x := t.fs.Folder(fd.Name)
					if x != nil {
						fd.Users = x.Users
					}
				}
			}
		}
	} else {
		x := t.fs.Folder(folderName)
		if x != nil {
			fd = *x
		} else {
			err = fmt.Errorf("folder %q not found", folderName)
		}
	}
	data := map[string]interface{}{
		"IsAdmin": t.isAdmin,
		"action":  "edit",
		"Request": r,
		"Error":   err,
		"Folder":  fd,
		"Now":     time.Now(),
	}
	err = t.tpl.createFolder.Execute(w, data)
	if err != nil {
		log.Printf("failed to serve create-folder handler: %v\n", err)
	}
}
func (t *torDropApp) RmFolder(w http.ResponseWriter, r *http.Request) {
	var err error
	var fd folder
	if r.Method == http.MethodPost {
		err = r.ParseForm()
		if err == nil {
			if err = t.decoder.Decode(&fd, r.Form); err == nil {
				err = t.fs.RmFolder(fd.Name)
				if err == nil {
					var url *url.URL
					url, err = t.router.Get("index").URL()
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					http.Redirect(w, r, url.String(), http.StatusSeeOther)
					return
				}
			}
		}
	}
	data := map[string]interface{}{
		"IsAdmin": t.isAdmin,
		"Request": r,
		"action":  "rm",
		"Error":   err,
		"Folder":  fd,
		"Now":     time.Now(),
	}
	err = t.tpl.createFolder.Execute(w, data)
	if err != nil {
		log.Printf("failed to serve create-folder handler: %v\n", err)
	}
}

var maxMemory = int64(10 << 20)

func (t *torDropApp) authFolderWithPassword(folderName string, pwd string, w http.ResponseWriter, r *http.Request) error {
	fd := t.fs.Folder(folderName)
	if fd == nil {
		return fmt.Errorf("folder %q not found", folderName)
	}

	if fd.Password != nil && *fd.Password != "" {
		loginOk := *fd.Password == pwd
		if loginOk {
			sess, err := t.session.Get(r, "pwd")
			if err != nil {
				return fmt.Errorf("failed to get session store pwd: %v", err)
			}
			sess.Values[folderName] = *fd.Password
			if err = sess.Save(r, w); err != nil {
				return fmt.Errorf("failed to save session store pwd: %v", err)
			}
			return nil
		}
		return fmt.Errorf("invalid password")
	}

	return nil
}

func (t *torDropApp) authFolderWithLogin(folderName string, w http.ResponseWriter, r *http.Request) error {
	fd := t.fs.Folder(folderName)
	if fd == nil {
		return fmt.Errorf("folder %q not found", folderName)
	}
	if fd.Users != nil && len(fd.Users) > 0 {
		var loginOk bool
		var user userLogin
		err := t.decoder.Decode(&user, r.Form)
		if err != nil {
			return err
		}
		for u, pwds := range fd.Users {
			if u == user.Login {
				for _, pwd := range pwds {
					if pwd == user.Password {
						loginOk = true
						break
					}
				}
			}
		}
		if loginOk {
			sess, err := t.session.Get(r, "user")
			if err != nil {
				return fmt.Errorf("failed to get session store user: %v", err)
			}
			sess.Values[folderName] = user
			if err = t.session.Save(r, w, sess); err != nil {
				return fmt.Errorf("failed to save session store user: %v", err)
			}
			return nil
		}

		return fmt.Errorf("invalid login")
	}
	return nil
}

func (t *torDropApp) hasAuthFolder(folderName string, w http.ResponseWriter, r *http.Request) error {
	fd := t.fs.Folder(folderName)
	if fd == nil {
		return fmt.Errorf("folder %q not found", folderName)
	}

	if fd.Password != nil && *fd.Password != "" {
		var loginOk bool
		sess, err := t.session.Get(r, "pwd")
		if err != nil {
			return fmt.Errorf("failed to get session store pwd: %v", err)
		}
		p := sess.Values[folderName]
		if p != nil {
			loginOk = p.(string) == *fd.Password
		}
		if !loginOk {
			return fmt.Errorf("invalid password")
		}
	} else if len(fd.Users) > 0 {
		var loginOk bool
		sess, err := t.session.Get(r, "user")
		if err != nil {
			return fmt.Errorf("failed to get session store user: %v", err)
		} else if y, ok := sess.Values[folderName]; ok && y != nil {
			p := y.(userLogin)
			if p.Login != "" && len(fd.Users[p.Login]) > 0 {
				for _, pwd := range fd.Users[p.Login] {
					if pwd == p.Password {
						loginOk = true
						break
					}
				}
			}
		}
		if !loginOk {
			return fmt.Errorf("invalid login")
		}
	}
	return nil
}

func (t *torDropApp) FolderListing(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodPost {
		r.ParseMultipartForm(maxMemory)
	}

	vars := mux.Vars(r)
	folderName := vars["folder"]
	fd := t.fs.Folder(folderName)

	var isValidLogin bool
	var err error
	if !t.isAdmin && fd != nil {

		if r.Method == http.MethodPost {
			if r.Form.Get("action") == "login" {
				err = t.authFolderWithPassword(folderName, r.Form.Get("Password"), w, r)
				if err == nil {
					isValidLogin = true
				}
			}
			if r.Form.Get("action") == "userlogin" {
				err = t.authFolderWithLogin(folderName, w, r)
				if err == nil {
					isValidLogin = true
				}
			}
		}

		if err == nil {
			err = t.hasAuthFolder(folderName, w, r)
			isValidLogin = err == nil
		}

		if err != nil {
			data := map[string]interface{}{
				"IsAdmin": t.isAdmin,
				"Request": r,
				"Folder":  fd,
				"Error":   err,
				"Now":     time.Now(),
			}
			err = t.tpl.folderLogin.Execute(w, data)
			if err != nil {
				log.Printf("failed to serve folder-login handler: %v\n", err)
			}
			return
		}
	}

	var passCaptcha bool
	if fd != nil {
		if (fd.Password != nil && *fd.Password != "") || len(fd.Users) > 0 {
			if !fd.CaptchaForLoggedUsers && isValidLogin {
				passCaptcha = true
			}
		} else if !fd.CaptchaForAnonymous {
			passCaptcha = true
		}
	}

	if t.isAdmin {
		passCaptcha = true
	}

	if r.Method == http.MethodPost {
		if t.isAdmin && r.Form.Get("action") == "rma" {
			err = t.fs.RmItem(fd.Name, r.Form.Get("Name"))

		} else if r.Form.Get("action") == "upload" {
			if passCaptcha == false {
				solution := r.Form.Get("Solution")
				captchaID := r.Form.Get("CaptchaID")
				if solution != "" && t.captchaSolution == solution {
					//OK
				} else if !captcha.VerifyString(captchaID, solution) {
					err = fmt.Errorf("invalid captcha solution")
				}
			}
			if err == nil {
				files := r.MultipartForm.File["files"]
				for i := range files {
					var src io.ReadCloser
					src, err = files[i].Open()
					if err == nil {
						fn := filepath.Base(files[i].Filename)
						if len(fn) > 220 {
							fn = fn[:220]
						}
						item := fileItem{
							CreateDate: time.Now(),
							Name:       fn,
							Path:       "/",
							Size:       uint64(files[i].Size),
						}
						err = t.fs.UploadItem(folderName, item, src)
					}
					if err != nil {
						break
					}
				}
				if err == nil {
					var u *url.URL
					u, err = t.router.Get("folder-listing").URL("folder", folderName)
					if err == nil {
						http.Redirect(w, r, u.String(), http.StatusSeeOther)
						return
					}
				}
			}
		}
	}

	var items fileItems
	if fd != nil {
		if t.isAdmin && fd.IsAdminOnlyReadable {
			x, e := t.fs.Items(folderName, true)
			items = x
			if err == nil {
				err = e
			}
		} else if !fd.IsAdminOnlyReadable {
			x, e := t.fs.Items(folderName, true)
			items = x
			if err == nil {
				err = e
			}
		}
	} else {
		http.NotFound(w, r)
		return
	}

	var c string
	if !passCaptcha {
		c = captcha.New()
	}

	data := map[string]interface{}{
		"IsAdmin":   t.isAdmin,
		"CaptchaID": c,
		"Request":   r,
		"Folder":    fd,
		"Items":     items,
		"Error":     err,
		"Now":       time.Now(),
	}
	err = t.tpl.folderListing.Execute(w, data)
	if err != nil {
		log.Printf("failed to serve folder-listing handler: %v\n", err)
	}
}
func (t *torDropApp) AssetDl(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)
	folderName := vars["folder"]
	fileName := vars["name"]
	fd := t.fs.Folder(folderName)
	if fd == nil {
		err = fmt.Errorf("folder %q not found", folderName)
	}

	if err == nil {
		// var isValidPassword bool
		// var isValidLogin bool
		if !t.isAdmin && fd != nil {
			if r.Method == http.MethodPost {
				if r.Form.Get("action") == "login" {
					err := t.authFolderWithPassword(folderName, r.Form.Get("Password"), w, r)
					if err == nil {
						// isValidPassword = true
					}
					t.logger.Error("folder %q auth with password failed: %v", folderName, err)
				}
				if r.Form.Get("action") == "userlogin" {

					err := t.authFolderWithLogin(folderName, w, r)
					if err != nil {
						t.logger.Error("folder %q auth with password failed: %v", folderName, err)
					} else {
						// isValidPassword = true
					}
				}
			}

			err := t.hasAuthFolder(folderName, w, r)
			if err != nil {
				data := map[string]interface{}{
					"IsAdmin": t.isAdmin,
					"Request": r,
					"Folder":  fd,
					"Error":   err,
					"Now":     time.Now(),
				}
				err = t.tpl.folderLogin.Execute(w, data)
				if err != nil {
					log.Printf("failed to serve folder-login handler: %v\n", err)
				}
				return
			}
		}
	}

	if err == nil {
		var src io.ReadCloser
		src, err = t.fs.OpenItem(folderName, fileName)
		if err == nil {
			w.Header().Add("Content-Type", "application/octet-stream")
			w.Header().Add("Content-Transfer-Encoding", "Binary")
			w.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			_, err = io.Copy(w, src)
			src.Close()
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
func (t *torDropApp) AssetInfo(w http.ResponseWriter, r *http.Request) {
	var err error
	vars := mux.Vars(r)
	folderName := vars["folder"]
	fileName := vars["name"]
	fd := t.fs.Folder(folderName)
	if fd == nil {
		err = fmt.Errorf("folder %q not found", folderName)
	}
	var fi fileItem
	if err == nil {
		fi, err = t.fs.Item(folderName, fileName)
	}
	data := map[string]interface{}{
		"IsAdmin": t.isAdmin,
		"Request": r,
		"Error":   err,
		"Folder":  fd,
		"File":    fi,
		"Now":     time.Now(),
	}
	err = t.tpl.assetInfo.Execute(w, data)
	if err != nil {
		log.Printf("failed to serve info-asset handler: %v\n", err)
	}
}

func (t *torDropApp) Mount(r *mux.Router) *mux.Router {
	t.router = r
	r.HandleFunc("/", t.Index).Name("index")
	r.HandleFunc("/list/{folder}", t.FolderListing).Name("folder-listing")
	if t.isAdmin {
		r.HandleFunc("/edit/{folder}", t.EditFolder).Name("folder-edit")
		r.HandleFunc("/rm/{folder}", t.RmFolder).Name("folder-rm")
		r.HandleFunc("/create", t.CreateFolder).Name("create-folder")
	}
	r.HandleFunc("/dl/{folder}/{name}", t.AssetDl).Name("asset-dl")
	r.Handle("/captcha/{id}.png", captcha.Server(150, 50)).Name("captcha")
	// r.HandleFunc("/info/{folder}/{name}", t.AssetInfo).Name("asset-info")

	if t.static {
		r.PathPrefix(t.assetsDir).
			Handler(http.StripPrefix(t.assetsDir, http.FileServer(assetFS())))
	} else {
		r.PathPrefix(t.assetsDir).
			Handler(http.StripPrefix(t.assetsDir, http.FileServer(http.Dir("."+t.assetsDir))))
	}

	return r
}
