package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
)

func showCreate(con *Context) {
	if con.r.Method == "POST" {
		addCreate(con)
		return
	}
	if con.r.Method != "GET" {
		con.w.WriteHeader(http.StatusMethodNotAllowed)
		con.w.Write([]byte("method is not supported"))
		return
	}
	if !con.CanEdit {
		con.w.WriteHeader(http.StatusForbidden)
		con.w.Write([]byte("no permission to change data"))
		return
	}
	con.Content = map[string]any{}

	primitives := []struct {
		name  string
		query string
	}{
		{"commands", "select id, name, updated, command, message from commands order by name"},
		{"checkers", "select id, name, description from checkers order by name"},
		{"notifier", "select id, name, settings from notifier order by name"},
		{"nodes", "select id, name, updated, message from nodes order by name"},
	}
	for _, prim := range primitives {
		rows, err := DB.Query(prim.query)
		defer rows.Close()
		if err != nil {
			log.Printf("could not get commands: %s", err)
			con.Error = "could not get commands"
			returnError(http.StatusInternalServerError, con, con.w)
			return
		}
		result, err := rowsToResult(rows)
		if err != nil {
			log.Printf("could not get %s: %s", prim.name, err)
			con.Error = "could not get " + prim.name
			returnError(http.StatusInternalServerError, con, con.w)
			return
		}
		con.Content[prim.name] = result
	}

	con.w.Header()["Content-Type"] = []string{"text/html"}
	con.Render("create_index")
	return
}

type (
	sqlResult struct {
		Columns []string
		Rows    [][]sql.NullString
	}
)

func rowsToResult(rows *sql.Rows) (*sqlResult, error) {
	res := &sqlResult{}
	cols, err := rows.Columns()
	if err != nil {
		return res, fmt.Errorf("could not get columns: %w", err)
	}
	res.Columns = cols
	res.Rows = [][]sql.NullString{}
	colNum := len(cols)

	for rows.Next() {
		line := make([]sql.NullString, colNum)
		lineMap := make([]any, colNum)
		for i := 0; i < colNum; i++ {
			lineMap[i] = &(line[i])
		}
		if err := rows.Scan(lineMap...); err != nil {
			return res, fmt.Errorf("could not scan values: %w", err)
		}
		res.Rows = append(res.Rows, line)
	}

	return res, nil
}

func addCreate(con *Context) {
	if !con.CanEdit {
		con.w.WriteHeader(http.StatusForbidden)
		con.w.Write([]byte("no permission to change data"))
		return
	}

	if err := con.r.ParseForm(); err != nil {
		con.w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(con.w, "could not parse parameters: %s", err)
		return
	}

	con.w.Header()["Location"] = []string{"/create"}

	addType := con.r.PostForm.Get("type")
	types := map[string]struct {
		Fields []string
		Table  string
	}{
		"command":  {[]string{"name", "command", "message"}, "commands"},
		"node":     {[]string{"name", "message"}, "nodes"},
		"checker":  {[]string{"name", "description"}, "checkers"},
		"notifier": {[]string{"name", "settings"}, "notifier"},
		"check":    {[]string{"name", "message", "options", "intval", "node_id", "command_id", "checker_id"}, "checks"},
	}
	t, found := types[addType]
	if !found {
		con.Error = "undefined type '" + addType + "'"
		returnError(http.StatusBadRequest, con, con.w)
		return
	}

	fields := make([]any, len(t.Fields))
	vals := make([]string, len(t.Fields))
	for i := 0; i < len(fields); i++ {
		vals[i] = fmt.Sprintf(`$%d`, i+1)
		fields[i] = con.r.PostForm.Get(t.Fields[i])
		if fields[i] == "" {
			con.Error = "field " + t.Fields[i] + " must not be empty"
			returnError(http.StatusBadRequest, con, con.w)
			return
		}
	}
	stmt := `insert into ` + t.Table + `(` + strings.Join(t.Fields, ",") + `) values (` + strings.Join(vals, ",") + `)`
	_, err := DB.Exec(stmt, fields...)
	if err != nil {
		log.Printf("could not insert new %s: %s", addType, err)
		con.Error = "could not insert new " + addType
		returnError(http.StatusInternalServerError, con, con.w)
		return
	}

	con.w.WriteHeader(http.StatusSeeOther)
	return
}
