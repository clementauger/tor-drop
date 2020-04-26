package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/azer/logger"
	"github.com/clementauger/tor-prebuilt/embedded"
	"github.com/cretz/bine/tor"
	"github.com/cretz/bine/torutil"
	tued25519 "github.com/cretz/bine/torutil/ed25519"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/gorilla/csrf"
	"github.com/gorilla/handlers"
	"github.com/gorilla/securecookie"
)

type torDropConfig struct {
	TmpDir           string
	MaxActiveUploads int
	StorageDir       string
}

type logWriter struct {
	*logger.Logger
}

func (l *logWriter) Printf(f string, args ...interface{}) {
	f = strings.TrimSuffix(f, "\n")
	l.Logger.Info(f, args...)
}

func newLogger(name string) *logWriter {
	return &logWriter{Logger: logger.New(name)}
}

func main() {

	var conf torDropConfig
	conf.StorageDir, _ = ioutil.TempDir("", "")
	conf.TmpDir, _ = ioutil.TempDir("", "")

	var pkpath string
	var secCookie string
	var secCsrf string
	var static bool
	var assetsDir string
	var storageDir string
	var qps float64
	if build == "dev" {
		secCookie = "static"
		secCsrf = "static"
	} else {
		secCookie = string(securecookie.GenerateRandomKey(32))
		secCsrf = string(securecookie.GenerateRandomKey(32))
	}
	flag.StringVar(&pkpath, "pk", "onion.pk", "ed25519 pem encoded privatekey file path")
	flag.StringVar(&secCookie, "cookie", secCookie, "secure cookie hashing secret")
	flag.StringVar(&secCsrf, "csrf", secCsrf, "secure csrf hashing secret")
	flag.StringVar(&storageDir, "storage", "data", "path to the storage directory")
	flag.StringVar(&assetsDir, "assets", "/assets/", "assets directory")
	flag.Float64Var(&qps, "qps", 30, "maximum http query per second")
	flag.BoolVar(&static, "static", true, "use embedded static assets")
	flag.Parse()

	if storageDir == "" {
		storageDir, _ = ioutil.TempDir("", "")
	}
	if secCookie == "" {
		secCookie = string(securecookie.GenerateRandomKey(32))
	}
	if secCsrf == "" {
		secCsrf = string(securecookie.GenerateRandomKey(32))
	}
	conf.StorageDir = storageDir

	fs := newFileServer(conf)
	admin, public, err := getApps(secCookie, fs, assetsDir, static, "")
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := fs.Listen(ctx)
		if err != nil {
			log.Fatalf("file server ended: %v", err)
		}
	}()

	lmt := tollbooth.NewLimiter(qps, &limiter.ExpirableOptions{DefaultExpirationTTL: time.Second})
	lmt.SetIPLookups([]string{"RemoteAddr", "X-Forwarded-For", "X-Real-IP"})

	var server serverListener
	var adminServer serverListener
	if build == "dev" {
		h := tollbooth.LimitHandler(lmt, public)
		h = handlers.LoggingHandler(os.Stdout, h)
		h = csrf.Protect([]byte(secCsrf))(h)
		server = &http.Server{
			Addr:    ":9090",
			Handler: h,
		}
		var hh http.Handler = admin
		hh = handlers.LoggingHandler(os.Stdout, hh)
		hh = csrf.Protect([]byte(secCsrf))(hh)
		adminServer = &http.Server{
			Addr:    ":9091",
			Handler: hh,
		}
		log.Println("public http://127.0.0.1:9090/")
		log.Println("admin  http://127.0.0.1:9091/")
	} else {
		h := tollbooth.LimitHandler(lmt, public)
		h = handlers.LoggingHandler(os.Stdout, h)
		h = csrf.Protect([]byte(secCsrf))(h)
		server = &torServer{
			PrivateKey:   pkpath,
			Handler:      h,
			ReadTimeout:  time.Hour,
			WriteTimeout: time.Hour,
		}
		var hh http.Handler = admin
		hh = handlers.LoggingHandler(os.Stdout, hh)
		hh = csrf.Protect([]byte(secCsrf))(hh)
		adminServer = &http.Server{
			Addr:         ":9091",
			Handler:      hh,
			ReadTimeout:  time.Hour,
			WriteTimeout: time.Hour,
		}
		log.Printf("public http://%v/\n", server.(*torServer).Onion())
		log.Println("admin  http://127.0.0.1:9091/")
	}

	errc := make(chan error)
	go func() {
		errc <- server.ListenAndServe()
	}()
	go func() {
		errc <- adminServer.ListenAndServe()
	}()

	sc := make(chan os.Signal)
	signal.Notify(sc, os.Interrupt)
	select {
	case err := <-errc:
		log.Println(err)
	case <-sc:
	}
}

func getOrCreatePK(fpath string) (ed25519.PrivateKey, error) {
	var privateKey ed25519.PrivateKey
	if _, err := os.Stat(fpath); os.IsNotExist(err) {
		_, privateKey, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		x509Encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			return nil, err
		}
		pemEncoded := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PRIVATE KEY", Bytes: x509Encoded})
		ioutil.WriteFile(fpath, pemEncoded, os.ModePerm)
	} else {
		d, _ := ioutil.ReadFile(fpath)
		block, _ := pem.Decode(d)
		x509Encoded := block.Bytes
		tPk, err := x509.ParsePKCS8PrivateKey(x509Encoded)
		if err != nil {
			return nil, err
		}
		if x, ok := tPk.(ed25519.PrivateKey); ok {
			privateKey = x
		} else {
			return nil, fmt.Errorf("invalid key type %T wanted ed25519.PrivateKey", tPk)
		}
	}
	return privateKey, nil
}

type serverListener interface {
	ListenAndServe() error
}

type torServer struct {
	Handler      http.Handler
	PrivateKey   string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func onion(pk ed25519.PrivateKey) string {
	return torutil.OnionServiceIDFromV3PublicKey(tued25519.PublicKey([]byte(pk.Public().(ed25519.PublicKey))))
}

func (ts *torServer) Onion() string {
	pk, err := getOrCreatePK(ts.PrivateKey)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%v.onion", onion(pk))
}

func (ts *torServer) ListenAndServe() error {

	pk, err := getOrCreatePK(ts.PrivateKey)
	if err != nil {
		return err
	}

	d, _ := ioutil.TempDir("", "data-dir")
	if err != nil {
		return err
	}

	t, err := tor.Start(nil, &tor.StartConf{
		DataDir:        d,
		ProcessCreator: embedded.NewCreator(),
		NoHush:         true,
	})
	if err != nil {
		return fmt.Errorf("unable to start Tor: %v", err)
	}
	defer t.Close()

	// Wait at most a few minutes to publish the service
	listenCtx, listenCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer listenCancel()
	// Create a v3 onion service to listen on any port but show as 80
	onion, err := t.Listen(listenCtx, &tor.ListenConf{Key: pk, Version3: true, RemotePorts: []int{80}})
	if err != nil {
		return fmt.Errorf("unable to create onion service: %v", err)
	}
	defer onion.Close()

	srv := &http.Server{
		ReadTimeout:  ts.ReadTimeout,
		WriteTimeout: ts.WriteTimeout,
		Handler:      ts.Handler,
	}
	return srv.Serve(onion)
}
