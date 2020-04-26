package main

import (
	"bytes"
	"context"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andrewstuart/limio"

	"github.com/gavv/httpexpect"
)

func TestBasics(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	eAdmin.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("No folder configured yet!")
	ePublic.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("No folder configured yet!")

	eAdmin.GET("/create").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("Create a new upload folder")
	ePublic.GET("/create").
		Expect().
		Status(http.StatusNotFound)

	var fd folderCreate
	fd.Folder.Name = "test"
	fd.Folder.CreateDate = time.Now()

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("Edit folder test")
	ePublic.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusNotFound)

	eAdmin.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/list/test\">test</a></td>")
	ePublic.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/list/test\">test</a></td>")

	eAdmin.GET("/list/test").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("This folder is currently empty!")
	ePublic.GET("/list/test").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("This folder is currently empty!")

	eAdmin.GET("/list/nop").
		Expect().
		Status(http.StatusNotFound)
	ePublic.GET("/list/nop").
		Expect().
		Status(http.StatusNotFound)

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "admin.txt", []byte("admin")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/test/admin.txt\" target=\"_blank\">admin.txt</a></td>")
	ePublic.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "public.txt", []byte("public")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/dl/test/admin.txt\" target=\"_blank\">admin.txt</a></td>").
		Contains("<td><a href=\"/dl/test/public.txt\" target=\"_blank\">public.txt</a></td>")

	eAdmin.GET("/dl/test/admin.txt").
		Expect().
		Status(http.StatusOK).
		Body().Equal("admin")
	eAdmin.GET("/dl/test/public.txt").
		Expect().
		Status(http.StatusOK).
		Body().Equal("public")
	ePublic.GET("/dl/test/public.txt").
		Expect().
		Status(http.StatusOK).
		Body().Equal("public")
	ePublic.GET("/dl/test/admin.txt").
		Expect().
		Status(http.StatusOK).
		Body().Equal("admin")

}

func TestCreateFolder(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, _, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)

	var fd folderCreate
	fd.Folder.Name = ""
	fd.Folder.CreateDate = time.Now()

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<b style=\"color:red\">folder name must not be empty</b>")
}

func TestMaxFileSize(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	type folderInput struct {
		Name        string
		MaxFileSize string
	}
	type folderCreateInput struct {
		Folder folderInput
	}

	var fd folderCreateInput
	fd.Folder.Name = "test"
	fd.Folder.MaxFileSize = "nono"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<b style=\"color:red\">schema: error converting value for &#34;Folder.MaxFileSize&#34;.")

	fd.Folder.Name = "test"
	fd.Folder.MaxFileSize = "250 b"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("Maximum size per file:\n      <input type=\"text\" name=\"Folder.MaxFileSize\" value=\"250 B\"")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "admin.txt", []byte("admin")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/test/admin.txt\" target=\"_blank\">admin.txt</a></td>")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "toolarge.txt", bytes.Repeat([]byte("toolarge"), 100)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		NotContains("<td><a href=\"/dl/test/toolarge.txt\" target=\"_blank\">toolarge.txt</a></td>").
		Contains("<b style=\"color:red\">the file too large, must not exceed 250 B</b>")

	ePublic.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "toolarge.txt", bytes.Repeat([]byte("toolarge"), 100)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("<td><a href=\"/dl/test/toolarge.txt\" target=\"_blank\">toolarge.txt</a></td>").
		Contains("<b style=\"color:red\">the file too large, must not exceed 250 B</b>")

}

func TestMaxFileCount(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	type folderInput struct {
		Name         string
		MaxFileCount string
	}
	type folderCreateInput struct {
		Folder folderInput
	}

	var fd folderCreateInput
	fd.Folder.Name = "test"
	fd.Folder.MaxFileCount = "nono"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<b style=\"color:red\">schema: error converting value for &#34;Folder.MaxFileCount&#34;</b>")

	fd.Folder.Name = "test"
	fd.Folder.MaxFileCount = "2"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("Maximum files in this folder:\n    <input type=\"text\" name=\"Folder.MaxFileCount\" placeholder=\"0 means no limit\"\n      value=\"2\" />")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file1.txt", []byte("file1")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/test/file1.txt\" target=\"_blank\">file1.txt</a></td>")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file2.txt", bytes.Repeat([]byte("file2"), 100)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/test/file2.txt\" target=\"_blank\">file2.txt</a></td>")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file3.txt", bytes.Repeat([]byte("file3"), 100)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		NotContains("<td><a href=\"/dl/test/file3.txt\" target=\"_blank\">file3.txt</a></td>").
		Contains("<b style=\"color:red\">this folder cannot accept more files</b>")

	ePublic.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file3.txt", bytes.Repeat([]byte("file3"), 100)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("<td><a href=\"/dl/test/file3.txt\" target=\"_blank\">file3.txt</a></td>").
		Contains("<b style=\"color:red\">this folder cannot accept more files</b>")

}

func TestMaxTotalSize(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	type folderInput struct {
		Name         string
		MaxTotalSize string
	}
	type folderCreateInput struct {
		Folder folderInput
	}

	var fd folderCreateInput
	fd.Folder.Name = "test"
	fd.Folder.MaxTotalSize = "nono"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<b style=\"color:red\">schema: error converting value for &#34;Folder.MaxTotalSize")

	fd.Folder.Name = "test"
	fd.Folder.MaxTotalSize = "20b"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("Maximum total size of this folder:\n      <input type=\"text\" name=\"Folder.MaxTotalSize\" value=\"20 B\"")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file1.txt", bytes.Repeat([]byte("file1"), 10)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<b style=\"color:red\">file is too large, demands 50 B, only 20 B available</b>")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file2.txt", bytes.Repeat([]byte("file2"), 2)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("10 B consumed of\n      20 B available").
		Contains("<td><a href=\"/dl/test/file2.txt\" target=\"_blank\">file2.txt</a></td>")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file3.txt", bytes.Repeat([]byte("file3"), 2)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("20 B consumed of\n      20 B available").
		Contains("<td><a href=\"/dl/test/file3.txt\" target=\"_blank\">file3.txt</a></td>")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file4.txt", bytes.Repeat([]byte("file4"), 2)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		NotContains("<td><a href=\"/dl/test/file4.txt\" target=\"_blank\">file4.txt</a></td>").
		Contains("<b style=\"color:red\">file is too large, demands 10 B, only 0 B available</b>")

	ePublic.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file4.txt", bytes.Repeat([]byte("file4"), 2)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("<td><a href=\"/dl/test/file4.txt\" target=\"_blank\">file4.txt</a></td>").
		Contains("<b style=\"color:red\">file is too large, demands 10 B, only 0 B available</b>")

}

func TestMaxLifeTime(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.UpdateInterval = time.Millisecond * 500
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, _, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)

	type folderInput struct {
		Name        string
		MaxLifeTime string
	}
	type folderCreateInput struct {
		Folder folderInput
	}

	var fd folderCreateInput
	fd.Folder.Name = "test"
	fd.Folder.MaxLifeTime = "nono"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<b style=\"color:red\">schema: error converting value for &#34;Folder.MaxLifeTime")

	fd.Folder.Name = "test"
	fd.Folder.MaxLifeTime = "1 second"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("Maximum file lifetime:\n      <input type=\"text\" name=\"Folder.MaxLifeTime\" value=\"1 second\"")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file1.txt", bytes.Repeat([]byte("file1"), 10)).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/test/file1.txt\" target=\"_blank\">file1.txt</a></td>")

	<-time.After(time.Second * 2)
	eAdmin.GET("/list/test").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		NotContains("<td><a href=\"/dl/test/file1.txt\" target=\"_blank\">file1.txt</a></td>")
}

func TestIsPrivate(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.UpdateInterval = time.Millisecond * 500
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "not-private"
	fd.Folder.CreateDate = time.Now()
	fd.Folder.IsPrivate = false

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("name=\"Folder.IsPrivate\" value=\"false\"\n        checked />")

	eAdmin.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/list/not-private\">not-private</a></td>")

	ePublic.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/list/not-private\">not-private</a></td>")

	fd.Folder.Name = "private"
	fd.Folder.CreateDate = time.Now()
	fd.Folder.IsPrivate = true

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("name=\"Folder.IsPrivate\" value=\"true\"\n        checked />")

	eAdmin.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/list/not-private\">not-private</a></td>").
		Contains("<td><a href=\"/list/private\">private</a></td>")

	ePublic.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/list/not-private\">not-private</a></td>").
		NotContains("<td><a href=\"/list/private\">private</a></td>")

}

func TestIsAdminOnlyReadable(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.UpdateInterval = time.Millisecond * 500
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "not-admin-only-readable"
	fd.Folder.CreateDate = time.Now()
	fd.Folder.IsAdminOnlyReadable = false

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("name=\"Folder.IsAdminOnlyReadable\" value=\"false\"\n        checked />")

	eAdmin.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/list/not-admin-only-readable\">not-admin-only-readable</a></td>")

	eAdmin.GET("/list/not-admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("is folder is currently empty")

	ePublic.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/list/not-admin-only-readable\">not-admin-only-readable</a></td>")

	ePublic.GET("/list/not-admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("is folder is currently empty")

	eAdmin.POST("/list/not-admin-only-readable").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "admin.txt", []byte("admin")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/not-admin-only-readable/admin.txt\" target=\"_blank\">admin.txt</a></td>")

	eAdmin.GET("/list/not-admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/not-admin-only-readable/admin.txt\" target=\"_blank\">admin.txt</a></td>").
		NotContains("is folder is currently empty")

	ePublic.GET("/list/not-admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/dl/not-admin-only-readable/admin.txt\" target=\"_blank\">admin.txt</a></td>").
		NotContains("is folder is currently empty")

	fd.Folder.Name = "admin-only-readable"
	fd.Folder.CreateDate = time.Now()
	fd.Folder.IsAdminOnlyReadable = true

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("name=\"Folder.IsAdminOnlyReadable\" value=\"true\"\n        checked />")

	eAdmin.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/list/admin-only-readable\">admin-only-readable</a></td>")

	eAdmin.GET("/list/admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("is folder is currently empty")

	ePublic.GET("/").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/list/admin-only-readable\">admin-only-readable</a></td>")

	ePublic.GET("/list/admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("is folder is currently empty")

	eAdmin.POST("/list/admin-only-readable").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "admin.txt", []byte("admin")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/admin-only-readable/admin.txt\" target=\"_blank\">admin.txt</a></td>")

	eAdmin.GET("/list/admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/admin-only-readable/admin.txt\" target=\"_blank\">admin.txt</a></td>").
		NotContains("is folder is currently empty")

	ePublic.GET("/list/admin-only-readable").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("<td><a href=\"/dl/admin-only-readable/admin.txt\" target=\"_blank\">admin.txt</a></td>").
		Contains("is folder is currently empty")
}

func TestMaxActiveUploads(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "test"
	fd.Folder.CreateDate = time.Now()
	max := 1
	fd.Folder.MaxActiveUploads = &max

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("type=\"text\" name=\"Folder.Name\" value=\"test").
		Contains("name=\"Folder.MaxActiveUploads\" placeholder=\"0 means no limit\"\n      value=\"1\"")

	data := bytes.Repeat([]byte("file4"), 1000)
	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file.txt", data).
		Expect().
		Status(http.StatusOK).
		Body()

	var wg sync.WaitGroup
	wg.Add(3)
	var b1 *httpexpect.String
	var b2 *httpexpect.String
	var b3 *httpexpect.String
	go func() {
		lr := limio.NewReader(bytes.NewReader(data))
		lr.SimpleLimit(1*limio.KB, time.Second)
		defer wg.Done()
		b1 = ePublic.GET("/dl/test/file.txt").
			Expect().
			Body()
	}()
	go func() {
		lr := limio.NewReader(bytes.NewReader(data))
		lr.SimpleLimit(1*limio.KB, time.Second)
		defer wg.Done()
		b2 = ePublic.GET("/dl/test/file.txt").
			Expect().
			Body()
	}()
	go func() {
		lr := limio.NewReader(bytes.NewReader(data))
		lr.SimpleLimit(1*limio.KB, time.Second)
		defer wg.Done()
		b3 = ePublic.GET("/dl/test/file.txt").
			Expect().
			Body()
	}()
	wg.Wait()

	k := "maximum active uploads exceeded, try again later"
	if !strings.Contains(b1.Raw(), k) &&
		!strings.Contains(b2.Raw(), k) &&
		!strings.Contains(b3.Raw(), k) {
		log.Println(b1.Raw())
		log.Println(b2.Raw())
		log.Println(b3.Raw())
		t.Fatal("limit was not exceeded")
	}

	if !strings.Contains(b1.Raw(), k) {
		if b1.Raw() != string(data) {
			log.Println(b1.Raw())
			t.Fatal("invalid response")
		}
	} else if !strings.Contains(b2.Raw(), k) {
		if b2.Raw() != string(data) {
			log.Println(b2.Raw())
			t.Fatal("invalid response")
		}

	} else if !strings.Contains(b3.Raw(), k) {
		if b3.Raw() != string(data) {
			log.Println(b3.Raw())
			t.Fatal("invalid response")
		}

	} else {
		t.Fatal("no way")
	}

	ePublic.GET("/dl/test/file.txt").
		Expect().
		Status(http.StatusOK).
		Body().Equal(string(data))
}

func TestMaxActiveDownloads(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "test"
	fd.Folder.CreateDate = time.Now()
	max := 1
	fd.Folder.MaxActiveDownloads = &max

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("type=\"text\" name=\"Folder.Name\" value=\"test").
		Contains("name=\"Folder.MaxActiveDownloads\" placeholder=\"0 means no limit\"\n      value=\"1\"")

	var wg sync.WaitGroup
	wg.Add(3)
	data := bytes.Repeat([]byte("file4"), 500)
	var b1 *httpexpect.String
	var b2 *httpexpect.String
	var b3 *httpexpect.String
	go func() {
		lr := limio.NewReader(bytes.NewReader(data))
		lr.SimpleLimit(1*limio.KB, time.Second)
		defer wg.Done()
		b1 = ePublic.POST("/list/test").
			WithMultipart().WithFormField("action", "upload").
			WithFile("files", "fileb1.txt", lr).
			Expect().
			Status(http.StatusOK).
			Body()
	}()
	go func() {
		lr := limio.NewReader(bytes.NewReader(data))
		lr.SimpleLimit(1*limio.KB, time.Second)
		defer wg.Done()
		b2 = ePublic.POST("/list/test").
			WithMultipart().WithFormField("action", "upload").
			WithFile("files", "fileb2.txt", lr).
			Expect().
			Status(http.StatusOK).
			Body()
	}()
	go func() {
		lr := limio.NewReader(bytes.NewReader(data))
		lr.SimpleLimit(1*limio.KB, time.Second)
		defer wg.Done()
		b3 = ePublic.POST("/list/test").
			WithMultipart().WithFormField("action", "upload").
			WithFile("files", "fileb3.txt", lr).
			Expect().
			Status(http.StatusOK).
			Body()
	}()
	wg.Wait()

	k := "<b style=\"color:red\">maximum active downloads exceeded, try again later</b>"
	if !strings.Contains(b1.Raw(), k) &&
		!strings.Contains(b2.Raw(), k) &&
		!strings.Contains(b3.Raw(), k) {
		log.Println(b1.Raw())
		log.Println(b2.Raw())
		log.Println(b3.Raw())
		t.Fatal("limit was not exceeded")
	}

	if !strings.Contains(b1.Raw(), k) {
		f := "<td><a href=\"/dl/test/fileb1.txt\" target=\"_blank\">fileb1.txt</a></td>"
		if !strings.Contains(b1.Raw(), f) {
			log.Println(b1.Raw())
			t.Fatal("fileb1 is not uploaded")
		}
	} else if !strings.Contains(b2.Raw(), k) {
		f := "<td><a href=\"/dl/test/fileb2.txt\" target=\"_blank\">fileb2.txt</a></td>"
		if !strings.Contains(b2.Raw(), f) {
			log.Println(b2.Raw())
			t.Fatal("fileb2 is not uploaded")
		}

	} else if !strings.Contains(b3.Raw(), k) {
		f := "<td><a href=\"/dl/test/fileb3.txt\" target=\"_blank\">fileb3.txt</a></td>"
		if !strings.Contains(b3.Raw(), f) {
			log.Println(b3.Raw())
			t.Fatal("fileb3 is not uploaded")
		}

	} else {
		t.Fatal("no way")
	}

	ePublic.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file3.txt", []byte("file4")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("<td><a href=\"/dl/test/file3.txt\" target=\"_blank\">file3.txt</a></td>").
		NotContains("style=\"color:red\">maximum active downloads exceeded, try again later</b>")
}

func TestUpdateFolder(t *testing.T) {
	// create date
	// name ?
}

func TestRmFolder(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	type folderInput struct {
		Name         string
		MaxTotalSize string
	}
	type folderCreateInput struct {
		Folder folderInput
	}

	var fd folderCreateInput
	fd.Folder.Name = "test"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("nput type=\"text\" name=\"Folder.Name\" value=\"test")

	eAdmin.POST("/list/test").
		WithMultipart().WithFormField("action", "upload").
		WithFileBytes("files", "file1.txt", []byte("file1")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td><a href=\"/dl/test/file1.txt\" target=\"_blank\">file1.txt</a></td>")

	eAdmin.GET("/dl/test/file1.txt").
		Expect().
		Status(http.StatusOK).
		Body().Equal("file1")

	fp := filepath.Join(conf.StorageDir, "test", "file1.txt")
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		t.Fatalf("file %q must exist", fp)
	}

	ePublic.POST("/rm/test").
		WithMultipart().WithFormField("Name", "test").
		Expect().
		Status(http.StatusNotFound)

	eAdmin.POST("/rm/test").
		WithFormField("Name", "test").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("No folder configured yet!")

	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Fatalf("file %q must not exist", fp)
	}

	fp = filepath.Join(conf.StorageDir, "test")
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Fatalf("file %q must not exist", fp)
	}

	eAdmin.POST("/rm/test").
		WithFormField("Name", "test").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<b style=\"color:red\">folder &#34;test&#34; not found</b>")

}

func TestFolderPassword(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.UpdateInterval = time.Millisecond * 500
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "withpwd"
	fd.Folder.CreateDate = time.Now()
	pwd := "tomate"
	fd.Folder.Password = &pwd

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("name=\"Folder.Password\" value=\"tomate\"")

	eAdmin.GET("/list/withpwd").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("is folder is currently empty")

	ePublic.GET("/list/withpwd").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("Login with the password").
		NotContains("is folder is currently empty").
		NotContains("Login with your credentials")

	ePublic.POST("/list/withpwd").
		WithFormField("action", "upload").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("Login with the password").
		NotContains("Login with your credentials").
		NotContains("is folder is currently empty")

	ePublic.POST("/list/withpwd").
		WithFormField("action", "login").
		WithFormField("Password", "tomate").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		NotContains("Login with your credentials").
		Contains("is folder is currently empty")

	ePublic.GET("/list/withpwd").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		NotContains("Login with your credentials").
		Contains("is folder is currently empty")

	ePublic.POST("/list/withpwd").
		WithMultipart().
		WithFormField("action", "upload").
		WithFileBytes("files", "/test.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		NotContains("Login with your credentials").
		NotContains("is folder is currently empty").
		Contains("<td><a href=\"/dl/withpwd/test.txt\" target=\"_blank\">test.txt</a></td>")

	ePublic.GET("/dl/withpwd/test.txt").
		Expect().
		Status(http.StatusOK).
		Body().
		Equal("test")

	ePublic2 := httpexpect.New(t, serverPublic.URL)

	ePublic2.GET("/dl/withpwd/test.txt").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("Login with the password").
		NotContains("Login with your credentials").
		NotContains("is folder is currently empty")
}

func TestFolderLogin(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.UpdateInterval = time.Millisecond * 500
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "withpwd"
	fd.Folder.CreateDate = time.Now()
	fd.User.Login = "tomate"
	fd.User.Password = "tomate"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("<td>tomate</td>\n      <td>[tomate]</td>")

	eAdmin.GET("/list/withpwd").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("is folder is currently empty")

	ePublic.GET("/list/withpwd").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		NotContains("is folder is currently empty").
		Contains("Login with your credentials")

	ePublic.POST("/list/withpwd").
		WithFormField("action", "upload").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		Contains("Login with your credentials").
		NotContains("is folder is currently empty")

	ePublic.POST("/list/withpwd").
		WithFormField("action", "userlogin").
		WithFormField("Login", "tomate").
		WithFormField("Password", "tomate").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		NotContains("Login with your credentials").
		Contains("is folder is currently empty")

	ePublic.GET("/list/withpwd").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		NotContains("Login with your credentials").
		Contains("is folder is currently empty")

	ePublic.POST("/list/withpwd").
		WithMultipart().
		WithFormField("action", "upload").
		WithFileBytes("files", "/test.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		NotContains("Login with your credentials").
		NotContains("is folder is currently empty").
		Contains("<td><a href=\"/dl/withpwd/test.txt\" target=\"_blank\">test.txt</a></td>")

	ePublic.GET("/dl/withpwd/test.txt").
		Expect().
		Status(http.StatusOK).
		Body().
		Equal("test")

	ePublic2 := httpexpect.New(t, serverPublic.URL)

	ePublic2.GET("/dl/withpwd/test.txt").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("Login with the password").
		Contains("Login with your credentials").
		NotContains("is folder is currently empty")
}

func TestCaptchaAnonymous(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.UpdateInterval = time.Millisecond * 500
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "solution")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "withcaptcha"
	fd.Folder.CreateDate = time.Now()
	fd.Folder.CaptchaForAnonymous = true

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("name=\"Folder.CaptchaForAnonymous\" value=\"true\"\n        checked")

	eAdmin.GET("/list/withcaptcha").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		NotContains("type=\"hidden\" name=\"CaptchaID\"value=")

	ePublic.GET("/list/withcaptcha").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("is folder is currently empty").
		Contains("type=\"hidden\" name=\"CaptchaID\"value=")

	eAdmin.POST("/list/withcaptcha").
		WithMultipart().
		WithFormField("action", "upload").
		WithFileBytes("files", "/test.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		NotContains("is folder is currently empty").
		Contains("<td><a href=\"/dl/withcaptcha/test.txt\" target=\"_blank\">test.txt</a></td>")

	ePublic.POST("/list/withcaptcha").
		WithMultipart().
		WithFormField("action", "upload").
		WithFileBytes("files", "/test-public.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("b style=\"color:red\">invalid captcha solution</b").
		NotContains("is folder is currently empty").
		Contains("<td><a href=\"/dl/withcaptcha/test.txt\" target=\"_blank\">test.txt</a></td>")

	ePublic.POST("/list/withcaptcha").
		WithMultipart().
		WithFormField("action", "upload").
		WithFormField("Solution", "solution").
		WithFileBytes("files", "/test-public.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("b style=\"color:red\">invalid captcha solution</b").
		NotContains("is folder is currently empty").
		Contains("<td><a href=\"/dl/withcaptcha/test.txt\" target=\"_blank\">test.txt</a></td>")
}

func TestCaptchaLoggedUser(t *testing.T) {

	secCookie := "sss"
	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	fs := newFileServer(conf)
	fs.UpdateInterval = time.Millisecond * 500
	fs.DataFile = filepath.Join(conf.TmpDir, "db.json")
	admin, public, err := getApps(secCookie, fs, "", false, "solution")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			t.Fatalf("file server ended: %v", err)
		}
	}()

	// run server using httptest
	serverAdmin := httptest.NewServer(admin)
	defer serverAdmin.Close()
	serverPublic := httptest.NewServer(public)
	defer serverPublic.Close()

	eAdmin := httpexpect.New(t, serverAdmin.URL)
	ePublic := httpexpect.New(t, serverPublic.URL)

	var fd folderCreate
	fd.Folder.Name = "withcaptcha"
	fd.Folder.CreateDate = time.Now()
	fd.Folder.CaptchaForLoggedUsers = true
	fd.User.Login = "tomate"
	fd.User.Password = "tomate"

	eAdmin.POST("/create").WithForm(fd).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("name=\"Folder.CaptchaForLoggedUsers\" value=\"true\"\n        checked")

	eAdmin.POST("/list/withcaptcha").
		WithFormField("action", "userlogin").
		WithFormField("Login", "tomate").
		WithFormField("Password", "tomate").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		Contains("is folder is currently empty").
		NotContains("type=\"hidden\" name=\"CaptchaID\"value=")

	ePublic.POST("/list/withcaptcha").
		WithFormField("action", "userlogin").
		WithFormField("Login", "tomate").
		WithFormField("Password", "tomate").
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("is folder is currently empty").
		Contains("type=\"hidden\" name=\"CaptchaID\"value=")

	eAdmin.POST("/list/withcaptcha").
		WithMultipart().
		WithFormField("action", "upload").
		WithFileBytes("files", "/test.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the administrator zone").
		NotContains("is folder is currently empty").
		NotContains("invalid captcha").
		Contains("<td><a href=\"/dl/withcaptcha/test.txt\" target=\"_blank\">test.txt</a></td>")

	ePublic.POST("/list/withcaptcha").
		WithMultipart().
		WithFormField("action", "upload").
		WithFileBytes("files", "/test2.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		Contains("invalid captcha").
		NotContains("<td><a href=\"/dl/withcaptcha/test2.txt\" target=\"_blank\">test2.txt</a></td>")

	ePublic.POST("/list/withcaptcha").
		WithMultipart().
		WithFormField("action", "upload").
		WithFormField("Solution", "solution").
		WithFileBytes("files", "/testpublic.txt", []byte("test")).
		Expect().
		Status(http.StatusOK).
		Body().
		Contains("Welcome to the public zone").
		NotContains("is folder is currently empty").
		Contains("<td><a href=\"/dl/withcaptcha/testpublic.txt\" target=\"_blank\">testpublic.txt</a></td>")

}
