package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/youngjae-lim/go-stripe/internal/driver"
	"github.com/youngjae-lim/go-stripe/internal/models"
)

const version = "1.0.0"

type config struct {
	port int
	env  string
	db   struct {
		dsn string
	}
	stripe struct {
		key    string
		secret string
	}
	smtp struct {
		host     string
		port     int
		username string
		password string
	}
}

type application struct {
	config   config
	infoLog  *log.Logger
	errorLog *log.Logger
	version  string
	DB       models.DBModel
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

	app.infoLog.Printf("Starting Backend server in %s mode on port %d", app.config.env, app.config.port)

	return srv.ListenAndServe()
}

func main() {
	var cfg config

	// set flags for config
	flag.IntVar(&cfg.port, "port", 4001, "Server port to listen on")
	flag.StringVar(&cfg.env, "env", "development", "{development|production|maintenance}")
	// parseTime=true enables the output type of DATE and DATETIME values to time.Time instead of []byte string
	// tls=false disables TLS/SSL encrypted connection to the server
	flag.StringVar(&cfg.db.dsn, "dsn", "youngjaelim:@tcp(localhost:3306)/widgets?parseTime=true&tls=false", "DSN")
	flag.StringVar(&cfg.smtp.host, "smtphost", "smtp.mailtrap.io", "smtp host")
	flag.StringVar(&cfg.smtp.username, "smtpusername", "b407d9befe3e65", "smtp username")
	flag.IntVar(&cfg.smtp.port, "smtpport", 587, "smtp port")

	// parse the flags
	flag.Parse()

	// key & secret are set in Makefile
	cfg.stripe.key = os.Getenv("STRIPE_KEY")
	cfg.stripe.secret = os.Getenv("STRIPE_SECRET")

	// mailtrap.io password
	cfg.smtp.password = os.Getenv("MAILTRAP_PASSWORD")

	infoLog := log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime)
	errorLog := log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime|log.Lshortfile)

	// connect to database
	conn, err := driver.OpenDB(cfg.db.dsn)
	if err != nil {
		errorLog.Fatal(err)
	}
	defer conn.Close()

	app := &application{
		config:   cfg,
		infoLog:  infoLog,
		errorLog: errorLog,
		version:  version,
		DB:       models.DBModel{DB: conn},
	}

	err = app.serve()
	if err != nil {
		app.errorLog.Println(err)
		log.Fatal(err)
	}
}
