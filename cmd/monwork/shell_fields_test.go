package main

import (
	"bytes"
	"fmt"
	"testing"
)

func TestStringToShellFields(t *testing.T) {
	type S struct {
		source string
		target []string
	}
	for i, e := range []S{
		S{"foo", []string{"foo"}},
		S{"foo bar", []string{"foo", "bar"}},
		S{`foo "bar"`, []string{"foo", `bar`}},
		S{`foo "bar baz"`, []string{"foo", `bar baz`}},
		S{`foo "bar" "baz"`, []string{"foo", `bar`, `baz`}},
		S{`foo "bar" baz`, []string{"foo", `bar`, `baz`}},
		S{`foo "bar 'hello" baz`, []string{"foo", `bar 'hello`, `baz`}},
		S{`foo "bar hello'" baz`, []string{"foo", `bar hello'`, `baz`}},
		S{`foo "bar 'hello'" baz`, []string{"foo", `bar 'hello'`, `baz`}},
	} {
		result := stringToShellFields([]byte(e.source))
		if err := compare(e.target, result); err != nil {
			t.Errorf("test %d did not match: %s", i, err)
		}
	}
}

func compare(source []string, target [][]byte) error {
	if len(source) != len(target) {
		return fmt.Errorf("length mismatch %d vs %d", len(source), len(target))
	}
	for i, e := range source {
		if bytes.Compare([]byte(e), target[i]) != 0 {
			return fmt.Errorf("mismatch in content field %d: %s vs %s", i, e, target[i])
		}
	}
	return nil
}
