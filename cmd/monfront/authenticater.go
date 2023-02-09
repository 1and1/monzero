package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	BasicAuthPrompt = `Basic realm="auth for monfront"`
	SessionCookie   = `session`
	UserAnonymous   = `anonymous`
)

type (
	// Authenticator is a middleware taking a context and authenticating
	// the user.
	Authenticator struct {
		db             *sql.DB
		Mode           string
		Token          []byte
		AllowAnonymous bool
		Header         string
		List           [][]string
		ClientCA       string

		sessions map[string]*session // maps a session key to a user
	}

	session struct {
		user string
		t    time.Time
	}
)

// Handler returns the handler for the authentication configuration.
func (a *Authenticator) Handler() (func(*Context) error, error) {
	switch a.Mode {
	case "none":
		return func(_ *Context) error { return nil }, nil
	case "header":
		if a.Header == "" {
			return nil, fmt.Errorf("authentication mode is 'header' but no header was provided")
		}
		return func(c *Context) error {
			if user := c.r.Header.Get(a.Header); user == "" {
				if a.AllowAnonymous {
					c.User = UserAnonymous
					return nil
				}
				return a.Unauthorized(c)
			} else {
				c.User = user
			}
			return nil
		}, nil
	case "list":
		return func(c *Context) error {
			user, pass, ok := c.r.BasicAuth()
			if !ok || user == "" || pass == "" {
				if a.AllowAnonymous {
					c.User = UserAnonymous
					return nil
				}
				c.w.Header().Set("WWW-Authenticate", BasicAuthPrompt)
				return a.Unauthorized(c)
			}
			var found string
			for _, entry := range a.List {
				if entry[0] == user {
					found = entry[1]
				}
			}
			if found == "" {
				c.w.Header().Set("WWW-Authenticate", BasicAuthPrompt)
				return a.Unauthorized(c)
			}
			p := pwHash{}
			if err := p.Parse(found); err != nil {
				log.Printf("could not parse hash for user '%s': %s", user, err)
				return a.Unauthorized(c)
			}
			if ok, err := p.compare(pass); err != nil {
				c.w.Header().Set("WWW-Authenticate", BasicAuthPrompt)
				return a.Unauthorized(c)
			} else if !ok {
				c.w.Header().Set("WWW-Authenticate", BasicAuthPrompt)
				return a.Unauthorized(c)
			}
			c.User = user
			return nil
		}, nil
	case "db":
		return func(c *Context) error {
			sessCookie := c.GetCookieVal(SessionCookie)
			if sessCookie != "" {
				ses := a.getSession(sessCookie)
				if ses != "" {
					// TODO fix time limit to make it variable
					c.SetCookie(SessionCookie, sessCookie, time.Now().Add(2*time.Hour))
					c.User = ses
					return nil
				}
			}
			return fmt.Errorf("NOT YET IMPLEMENTED")
		}, fmt.Errorf("NOT YET IMPLEMENTED")
	case "cert":
		return func(c *Context) error {
			return fmt.Errorf("NOT YET IMPLEMENTED")
		}, fmt.Errorf("NOT YET IMPLEMENTED")
	default:
		return nil, fmt.Errorf("unknown mode '%s' for authentication", a.Mode)
	}
	return nil, fmt.Errorf("could not create authenticator")
}

func (a *Authenticator) Unauthorized(c *Context) error {
	c.w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(c.w, "unauthorized\n")
	return fmt.Errorf("no authentication")
}

// creates a session for a user
func (a *Authenticator) createSession(user string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("could not generate new session key")
	}
	res := a.mac(raw)
	ses := fmt.Sprintf(
		"%s-%s",
		base64.StdEncoding.EncodeToString(raw),
		base64.StdEncoding.EncodeToString(res),
	)
	a.sessions[ses] = &session{user: user, t: time.Now()}
	return ses, nil
}

func (a *Authenticator) mac(input []byte) []byte {
	mac := hmac.New(sha256.New, a.Token)
	mac.Write(input)
	return mac.Sum(nil)
}

// getSession returns the username of the current session.
func (a *Authenticator) getSession(session string) string {
	if session == "" {
		return ""
	}
	parts := strings.Split(session, "-")
	if len(parts) != 2 {
		return ""
	}
	msg, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	mac, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	verify := a.mac(msg)
	if !hmac.Equal(mac, verify) {
		return ""
	}
	if ses, found := a.sessions[session]; found {
		// TODO make timeout a config option
		if time.Now().Sub(ses.t) < 8*time.Hour {
			delete(a.sessions, session)
			return ""
		}
		ses.t = time.Now()
		return ses.user
	}
	return ""
}
