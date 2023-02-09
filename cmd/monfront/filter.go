package main

import (
	"fmt"
	"strings"
)

type (
	filter struct {
		idx    int
		where  []string
		params []interface{}
		Vals   map[string]string
	}
)

func newFilter() *filter {
	return &filter{
		idx:    0,
		where:  []string{},
		params: []interface{}{},
		Vals:   map[string]string{},
	}
}

func (f *filter) filterChecks(c *Context) {
	args := c.r.URL.Query()
	for name, val := range args {
		if !strings.HasPrefix(name, "filter-") {
			continue
		}
		arg := strings.TrimPrefix(name, "filter-")
		switch arg {
		case "command":
			if val[0] == "" {
				continue
			}
			f.Add("co.id", "=", val[0], "int")
			f.Vals[arg] = val[0]
		case "search":
			if val[0] == "" {
				continue
			}
			f.Add(`n.name`, `like`, strings.ReplaceAll(val[0], "*", "%"), "text")
			f.Vals[arg] = val[0]
		case "state":
			if val[0] == "" {
				continue
			}
			f.Add("states[1]", ">=", val[0], "int")
			f.Vals[arg] = val[0]
		case "ack":
			if val[0] == "" {
				continue
			}
			if val[0] != "true" && val[0] != "false" {
				continue
			}
			f.Add("acknowledged", "=", val[0], "boolean")
			f.Vals[arg] = val[0]
		case "mapping":
			if val[0] == "" {
				continue
			}
			f.Add("ac.mapping_id", "=", val[0], "int")
			f.Vals[arg] = val[0]
		}
	}
}

// Add a new where clause element which will be joined at the end.
func (f *filter) Add(field, op string, arg interface{}, castTo string) {
	f.idx += 1
	f.where = append(f.where, fmt.Sprintf("%s %s $%d::%s", field, op, f.idx, castTo))
	f.params = append(f.params, arg)
}

// AddSpecial lets you add a special where clause comparison where you can
// wrap the argument in whatevery you like.
//
// Your string has to contain %d. This will place the index of the variable
// in the query string.
//
// Example:
//	AddSpecial("foo", "=", "to_tsvector('english', $%d), search)
func (f *filter) AddSpecial(field, op, special string, arg interface{}) {
	f.idx += 1
	f.where = append(f.where, fmt.Sprintf("%s %s "+special, field, op, f.idx))
	f.params = append(f.params, arg)
}

// Join takes all where clauses and joins them together with the AND operator.
// The result and all collected parameters are then returned.
func (f *filter) Join() (string, []interface{}) {
	return strings.Join(f.where, " and "), f.params
}
