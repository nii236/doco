package doco

import (
	"bytes"
	"context"
	"doco/db"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"net/http"
	"text/template"

	"github.com/caddyserver/caddy"
	// http driver for caddy
	_ "github.com/caddyserver/caddy/caddyhttp"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"
)

var sessionManager *scs.SessionManager

// ErrNotImplemented is used to stub empty funcs
var ErrNotImplemented = errors.New("not implemented")

// ErrUnableToPopulate occurs because of SQLite's ID creation order
var ErrUnableToPopulate = "db: unable to populate default values"

// SecureHandlerFunc is a custom http.HandlerFunc that returns a status code and error
type SecureHandlerFunc func(w http.ResponseWriter, r *http.Request) (interface{}, int, error)

// HandlerFunc is a custom http.HandlerFunc that returns a status code and error
type HandlerFunc func(w http.ResponseWriter, r *http.Request) (interface{}, int, error)

// ErrorResponse for HTTP
type ErrorResponse struct {
	Err     string `json:"err"`
	Message string `json:"message"`
}

// Err constructor
func Err(err error, message ...string) *ErrorResponse {
	e := &ErrorResponse{
		Err: err.Error(),
	}
	e.Message = err.Error()
	if len(message) > 0 {
		e.Message = message[0]
	}
	return e
}

// Unwrap the inner error
func (e *ErrorResponse) Unwrap() error {
	return errors.New(e.Err)
}
func (e *ErrorResponse) Error() string {
	return e.Message
}

// JSON body for HTTP response
func (e *ErrorResponse) JSON() string {
	b, err := json.Marshal(e)
	if err != nil {
		fmt.Println(err)
		return ""
	}
	return string(b)
}
func withError(next HandlerFunc) http.HandlerFunc {
	fn := func(w http.ResponseWriter, r *http.Request) {
		result, code, err := next(w, r)
		if err != nil {
			fmt.Println(err)
			http.Error(w, Err(err).JSON(), code)
			return
		}
		if result == nil {
			if err == nil {
				err = errors.New("no response")
			}
			fmt.Println(err)
			http.Error(w, Err(err, "no response").JSON(), code)
			return
		}
		err = json.NewEncoder(w).Encode(result)
		if err != nil {
			fmt.Println(err)
			http.Error(w, Err(err).JSON(), code)
			return
		}
		return
	}
	return fn
}

const caddyfileTemplate = `
{{ .caddyAddr}} {
	tls off
    proxy /api/ localhost{{ .apiAddr }} {
		transparent
		websocket
		timeout 10m
    }
    root {{ .rootPath }}
    rewrite { 
        if {path} not_match ^/api
        to {path} /
    }
}
`

// RunServer the service
func RunServer(ctx context.Context, conn *sqlx.DB, serverAddr string, jwtsecret string, log *zap.SugaredLogger) error {
	log.Infow("start api", "svc-addr", serverAddr)
	c := &API{log}

	cors := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	})

	r := chi.NewRouter()
	r.Use(cors.Handler)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Route("/api", func(r chi.Router) {
		// Authenticated routes
		r.Group(func(r chi.Router) {
			r.Get("/blobs/{blob_id}", c.blobHandler())
		})

		// Public routes
		r.Group(func(r chi.Router) {
			r.Get("/metrics", promhttp.Handler().ServeHTTP)
		})

	})

	return http.ListenAndServe(serverAddr, sessionManager.LoadAndSave(r))
}

type API struct {
	log *zap.SugaredLogger
}

// RunLoadBalancer starts Caddy
func RunLoadBalancer(ctx context.Context, conn *sqlx.DB, loadBalancerAddr, serverAddr, rootPath string, log *zap.SugaredLogger) error {
	log.Infow("start load balancer", "lb-addr", loadBalancerAddr, "svc-addr", serverAddr, "web", rootPath)
	caddy.AppName = "Doco"
	caddy.AppVersion = "0.0.1"
	caddy.Quiet = true
	t := template.Must(template.New("CaddyFile").Parse(caddyfileTemplate))
	data := map[string]string{
		"caddyAddr": loadBalancerAddr,
		"apiAddr":   serverAddr,
		"rootPath":  rootPath,
	}

	result := &bytes.Buffer{}
	err := t.Execute(result, data)
	if err != nil {
		return err
	}
	caddyfile := &caddy.CaddyfileInput{
		Contents:       result.Bytes(),
		Filepath:       "Caddyfile",
		ServerTypeName: "http",
	}

	instance, err := caddy.Start(caddyfile)
	if err != nil {
		return err
	}
	instance.Wait()
	return nil
}
func (c *API) checkHandler() func(w http.ResponseWriter, r *http.Request) (interface{}, int, error) {
	fn := func(w http.ResponseWriter, r *http.Request) (interface{}, int, error) {
		type Response struct {
		}

		return &Response{}, 200, nil
	}
	return fn

}

func (c *API) blobHandler() func(w http.ResponseWriter, r *http.Request) {
	fn := func(w http.ResponseWriter, r *http.Request) {
		blobFilename := chi.URLParam(r, "blob_id")
		blob, err := db.Blobs(db.BlobWhere.FileName.EQ(blobFilename)).OneG()
		if err != nil {
			http.Error(w, Err(err).JSON(), http.StatusBadRequest)
			return
		}

		// tell the browser the returned content should be downloaded/inline
		if blob.MimeType != "" && blob.MimeType != "unknown" {
			w.Header().Add("Content-Type", blob.MimeType)
		}
		w.Header().Add("Content-Disposition", fmt.Sprintf("%s;filename=%s", "attachment", blob.FileName))
		rdr := bytes.NewReader(blob.File)
		http.ServeContent(w, r, blob.FileName, time.Now(), rdr)
		return
	}
	return fn
}

func (c *API) metricsHandler(w http.ResponseWriter, r *http.Request) (interface{}, int, error) {
	return nil, 200, ErrNotImplemented
}
