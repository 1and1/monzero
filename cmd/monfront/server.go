package main

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type (
	server struct {
		listen net.Listener
		db     *sql.DB
		h      *http.ServeMux
		tmpl   *template.Template
		auth   func(c *Context) error // authentication
		autho  func(c *Context) error // authorization
	}

	handleFunc func(c *Context)

	Context struct {
		// internal maintenance stuff
		w    http.ResponseWriter
		r    *http.Request
		tmpl *template.Template
		db   *sql.DB

		User    string  `json:"-"`
		Filter  *filter `json:"-"`
		CanEdit bool    `json:"-"` // has user permission to edit stuff?

		Title        string                   `json:"title,omitempty"`
		CurrentPath  string                   `json:"-"`
		Error        string                   `json:"error,omitempty"`
		Mappings     map[int]map[int]MapEntry `json:"mappings,omitempty"`
		Commands     map[string]int           `json:"commands,omitempty"`
		Checks       []check                  `json:"checks,omitempty"`
		CheckDetails *checkDetails            `json:"check_details,omitempty"`
		Groups       []group                  `json:"groups,omitempty"`
		Unhandled    bool                     `json:"-"` // set this flag when unhandled was called

		Content map[string]any `json:"-"` // used for the configuration dashboard
	}
)

func newServer(l net.Listener, db *sql.DB, tmpl *template.Template, auth func(c *Context) error, autho func(c *Context) error) *server {
	s := &server{
		listen: l,
		db:     db,
		tmpl:   tmpl,
		h:      http.NewServeMux(),
		auth:   auth,
		autho:  autho,
	}
	return s
}

func (s *server) ListenAndServe() error {
	server := http.Server{Handler: s.h}
	return server.Serve(s.listen)
}

func (s *server) Handle(path string, fun handleFunc) {
	s.h.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		c := &Context{
			w:    w,
			r:    r,
			tmpl: s.tmpl,
			db:   s.db,
		}
		if err := s.auth(c); err != nil {
			return
		}
		if err := s.autho(c); err != nil {
			return
		}
		fun(c)
		return
	})
}

func (s *server) HandleStatic(path string, h func(w http.ResponseWriter, r *http.Request)) {
	s.h.HandleFunc(path, h)
}

// Render calls the template with the given name to
// render the appropiate content.
// In case of an error, a error message is automatically pushed
// to the client.
func (c *Context) Render(t string) error {
	var w io.Writer = c.w
	if strings.Contains(c.r.Header.Get("Accept-Encoding"), "gzip") {
		gz, err := gzip.NewWriterLevel(w, 5)
		if err != nil {
			log.Printf("could not create gzip writer: %s", err)
			return fmt.Errorf("could not create gzip writer: %s", err)
		}
		defer gz.Close()
		w = gz
		c.w.Header().Set("Content-Encoding", "gzip")
	}

	if c.r.Header.Get("Accept") == "application/json" {
		c.w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "") // disable indentation to save traffic
		if err := enc.Encode(c); err != nil {
			c.w.WriteHeader(http.StatusInternalServerError)
			c.w.Write([]byte("could not write json output"))
			log.Printf("could not write json output: %s", err)
			return err
		}
		return nil
	}

	if err := c.tmpl.ExecuteTemplate(w, t, c); err != nil {
		c.w.WriteHeader(http.StatusInternalServerError)
		c.w.Write([]byte("problem with a template"))
		log.Printf("could not execute template: %s", err)
		return err
	}
	return nil
}

// Get a cookie value.
func (c *Context) GetCookieVal(name string) string {
	cook, err := c.r.Cookie(name)
	if err == http.ErrNoCookie {
		return ""
	}
	return cook.Value
}

// Set a new key value cookie with a deadline.
func (c *Context) SetCookie(name, val string, expire time.Time) {
	cook := http.Cookie{
		Name:     name,
		Value:    val,
		Expires:  expire,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
		Path:     "/",
	}
	http.SetCookie(c.w, &cook)
	return
}
