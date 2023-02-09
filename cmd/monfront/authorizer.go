package main

import (
	"database/sql"
	"fmt"
)

type (
	Authorizer struct {
		db   *sql.DB
		Mode string
		List []string
	}
)

func (a *Authorizer) Handler() (func(c *Context) error, error) {
	switch a.Mode {
	case "none":
		return func(_ *Context) error { return nil }, nil
	case "list":
		return func(c *Context) error {
			for _, user := range a.List {
				if user == c.User {
					c.CanEdit = true
					return nil
				}
			}
			return nil
		}, nil
	case "all":
		return func(c *Context) error { c.CanEdit = true; return nil }, nil
	default:
		return func(_ *Context) error { return nil }, fmt.Errorf("authorization mode '%s' is unsupported", a.Mode)
	}
}
