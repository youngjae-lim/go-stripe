package main

import (
	"encoding/gob"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/v2"
	"github.com/youngjae-lim/go-stripe/internal/driver"
	"github.com/youngjae-lim/go-stripe/internal/models"
)

const version = "1.0.0"
const cssVersion = "1"

var session *scs.SessionManager

type config struct {
	port int
	env  string
	api  string
	db   struct {
		dsn string
	}
	stripe struct {
		key    string
		secret string
	}
	pwreset_secretkey string
	frontend_url      string
}

type application struct {
	config        config
	infoLog       *log.Logger
	errorLog      *log.Logger
	templateCache map[string]*template.Template
	version       string
	DB            models.DBModel
	Session       *scs.SessionManager
}

func (app *application) serve() error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", app.config.port),
		Handler:           app.routes(),
		IdleTimeout:       30 * time.Second,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}

	app.infoLog.Printf("Starting HTTP server in %s mode on port %d", app.config.env, app.config.port)

	return srv.ListenAndServe()
}

func main() {
	// To encode/decode a map[string]interface{}, since the field of the map is enclosed as interface type, we need to register the specific type in advance
	gob.Register(TransanctionData{})

	var cfg config

	flag.IntVar(&cfg.port, "port", 4000, "Server port to listen on")
	flag.StringVar(&cfg.env, "env", "development", "{development|production}")
	flag.StringVar(&cfg.api, "api", "http://localhost:4001", "URL to api")
	// parseTime=true enables the output type of DATE and DATETIME values to time.Time instead of []byte string
	// tls=false disables TLS/SSL encrypted connection to the server
	flag.StringVar(&cfg.db.dsn, "dsn", "youngjaelim:@tcp(localhost:3306)/widgets?parseTime=true&tls=false", "DSN")
	flag.StringVar(&cfg.pwreset_secretkey, "pwreset_skey", "Test1234!", "password reset secret key")
	flag.StringVar(&cfg.frontend_url, "frontend_url", "http://localhost:4000", "url to front end")

	flag.Parse()

	// Please make sure to set STRIPE_KEY in the .air.toml file
	// if you want to run the app with air for live-loading
	// key & secret are set in Makefile as well so we can still grab them
	// when not using air
	cfg.stripe.key = os.Getenv("STRIPE_KEY")
	cfg.stripe.secret = os.Getenv("STRIPE_SECRET")

	infoLog := log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	errorLog := log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	// connect to database
	conn, err := driver.OpenDB(cfg.db.dsn)
	if err != nil {
		errorLog.Fatal(err)
	}
	defer conn.Close()

	// Initialize a new session manager and configure it to use MySQL as the session store
	session = scs.New()
	session.Lifetime = 24 * time.Hour
	session.Store = mysqlstore.New(conn)

	tc := make(map[string]*template.Template)

	app := &application{
		config:        cfg,
		infoLog:       infoLog,
		errorLog:      errorLog,
		templateCache: tc,
		version:       version,
		DB:            models.DBModel{DB: conn},
		Session:       session,
	}

	err = app.serve()
	if err != nil {
		app.errorLog.Println(err)
		log.Fatal(err)
	}
}
