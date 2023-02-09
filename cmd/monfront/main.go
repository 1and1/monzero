package main

import (
	"crypto/tls"
	"database/sql"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/lib/pq"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	configPath = flag.String("config", "monfront.conf", "path to the config file")
	DB         *sql.DB
	Tmpl       *template.Template
)

type (
	Config struct {
		DB           string `toml:"db"`
		Listen       string `toml:"listen"`
		TemplatePath string `toml:"template_path"`
		SSL          struct {
			Enable bool   `toml:"enable"`
			Priv   string `toml:"private_key"`
			Cert   string `toml:"certificate"`
		} `toml:"ssl"`
		Authentication struct {
			Mode           string     `toml:"mode"`
			Token          string     `toml:"session_token"`
			AllowAnonymous bool       `toml:"allow_anonymous"`
			Header         string     `toml:"header"`
			List           [][]string `toml:"list"`
			ClientCA       string     `toml:"cert"`
		} `toml:"authentication"`
		Authorization struct {
			Mode string   `toml:"mode"`
			List []string `toml:"list"`
		}
	}

	MapEntry struct {
		Name  string
		Title string
		Color string
	}
)

func main() {
	flag.Parse()

	if len(flag.Args()) > 0 {
		switch flag.Arg(0) {
		case "pwgen":
			fmt.Printf("enter password: ")
			pw, err := terminal.ReadPassword(0)
			fmt.Println()
			if err != nil {
				log.Fatalf("could not read password: %s", err)
			}
			hash, err := newHash(string(pw))
			if err != nil {
				log.Fatalf("could not generate password hash: %s", err)
			}
			fmt.Printf("generated password hash: %s\n", hash)
			os.Exit(0)
		default:
			log.Fatalf("unknown command '%s'", flag.Arg(0))
		}
	}

	if info, err := os.Stat(*configPath); err != nil {
		log.Fatalf("could not find config '%s': %s", *configPath, err)
	} else if info.Mode() != 0600 && info.Mode() != 0400 {
		log.Fatalf("config '%s' is world readable!", *configPath)
	}

	raw, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("could not read config: %s", err)
	}
	config := Config{
		Listen:       "127.0.0.1:8080",
		TemplatePath: "templates",
	}
	if err := toml.Unmarshal(raw, &config); err != nil {
		log.Fatalf("could not parse config: %s", err)
	}

	db, err := sql.Open("postgres", config.DB)
	if err != nil {
		log.Fatalf("could not open database connection: %s", err)
	}
	DB = db

	authenticator := Authenticator{
		db:             db,
		Mode:           config.Authentication.Mode,
		Token:          []byte(config.Authentication.Token),
		AllowAnonymous: config.Authentication.AllowAnonymous,
		Header:         config.Authentication.Header,
		List:           config.Authentication.List,
		ClientCA:       config.Authentication.ClientCA,
	}
	auth, err := authenticator.Handler()
	if err != nil {
		log.Fatalf("could not start authenticator")
	}
	authorizer := Authorizer{
		db:   db,
		Mode: config.Authorization.Mode,
		List: config.Authorization.List,
	}
	autho, err := authorizer.Handler()
	if err != nil {
		log.Fatalf("could not start authorizer")
	}

	tmpl := template.New("main")
	tmpl.Funcs(Funcs)
	files, err := ioutil.ReadDir(config.TemplatePath)
	if err != nil {
		log.Fatalf("could not read directory '%s': %s", config.TemplatePath, err)
	}
	for _, file := range files {
		if !file.Mode().IsRegular() {
			continue
		}
		if !strings.HasSuffix(file.Name(), ".html") {
			continue
		}
		raw, err := ioutil.ReadFile(path.Join(config.TemplatePath, file.Name()))
		if err != nil {
			log.Fatalf("could not read file '%s': %s", path.Join(config.TemplatePath, file.Name()), err)
		}
		template.Must(tmpl.New(strings.TrimSuffix(file.Name(), ".html")).Parse(string(raw)))
	}
	Tmpl = tmpl

	if config.Listen == "" {
		config.Listen = "127.0.0.1:8080"
	}
	l, err := net.Listen("tcp", config.Listen)
	if err != nil {
		log.Fatalf("could not create listener: %s", err)
	}
	if config.SSL.Enable {
		cert, err := tls.LoadX509KeyPair(config.SSL.Cert, config.SSL.Priv)
		if err != nil {
			log.Fatalf("could not load certificate: %s", err)
		}
		tlsConf := &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2", "1.1"},
		}
		l = tls.NewListener(l, tlsConf)
	}

	s := newServer(l, db, tmpl, auth, autho)
	s.Handle("/", showChecks)
	s.Handle("/create", showCreate)
	s.Handle("/check", showCheck)
	s.Handle("/checks", showChecks)
	s.Handle("/groups", showGroups)
	s.Handle("/action", checkAction)
	s.HandleStatic("/static/", showStatic)
	log.Fatalf("http server stopped: %s", s.ListenAndServe())
}

func checkAction(con *Context) {
	if con.r.Method != "POST" {
		con.w.WriteHeader(http.StatusMethodNotAllowed)
		con.w.Write([]byte("method is not supported"))
		return
	}
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
	ref, found := con.r.Header["Referer"]
	if found {
		con.w.Header()["Location"] = ref
	} else {
		con.w.Header()["Location"] = []string{"/"}
	}
	checks := con.r.PostForm["checks"]
	action := con.r.PostForm.Get("action")
	if action == "" || len(checks) == 0 {
		con.w.WriteHeader(http.StatusSeeOther)
		return
	}
	setTable := "checks"
	setClause := ""

	comment := con.r.PostForm.Get("comment")
	run_in := con.r.PostForm.Get("run_in")
	if action == "comment" && comment == "" && run_in != "" {
		action = "reschedule"
	}

	whereFields := []string{}
	whereVals := []any{}

	switch action {
	case "mute":
		setTable = "checks_notify"
		setClause = "enabled = false"
	case "unmute":
		setTable = "checks_notify"
		setClause = "enabled = true"
	case "enable":
		setClause = "enabled = true, updated = now()"
	case "disable":
		setClause = "enabled = false, updated = now()"
	case "delete_check":
		if _, err := DB.Exec(`delete from checks where id = any ($1::bigint[])`, pq.Array(checks)); err != nil {
			log.Printf("could not delete checks '%s': %s", checks, err)
			con.Error = "could not delete checks"
			returnError(http.StatusInternalServerError, con, con.w)
			return
		}
		con.w.WriteHeader(http.StatusSeeOther)
		return
	case "create_check":

	case "reschedule":
		setClause = "next_time = now()"
		if run_in != "" {
			runNum, err := strconv.Atoi(run_in)
			if err != nil {
				con.Error = "run_in is not a valid number"
				returnError(http.StatusBadRequest, con, con.w)
				return
			}
			setClause = fmt.Sprintf("next_time = now() + '%dmin'::interval", runNum)
		}
		setTable = "active_checks"
	case "deack":
		setClause = "acknowledged = false"
		setTable = "active_checks"
	case "ack":
		setClause = "acknowledged = true"
		setTable = "active_checks"
		whereFields = append(whereFields, "states[0]")
		whereVals = append(whereVals, 0)

		hostname, err := os.Hostname()
		if err != nil {
			log.Printf("could not resolve hostname: %s", err)
			con.Error = "could not resolve hostname"
			returnError(http.StatusInternalServerError, con, con.w)
			return
		}
		if _, err := DB.Exec(`insert into notifications(check_id, states, output, mapping_id, notifier_id, check_host)
			select ac.check_id, 0 || states[1:4], 'check acknowledged', ac.mapping_id,
			cn.notifier_id, $2
			from checks_notify cn
			join active_checks ac on cn.check_id = ac.check_id
			where cn.check_id = any ($1::bigint[])`, pq.Array(&checks), &hostname); err != nil {
			log.Printf("could not acknowledge check: %s", err)
			con.Error = "could not acknowledge check"
			returnError(http.StatusInternalServerError, con, con.w)
			return
		}
	case "comment":
		if comment == "" {
			con.w.WriteHeader(http.StatusSeeOther)
			return
		}
		_, err := DB.Exec(
			"update active_checks set notice = $2 where check_id = any ($1::bigint[]);",
			pq.Array(&checks),
			comment)
		if err != nil {
			con.w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(con.w, "could not store changes")
			log.Printf("could not adjust checks %#v: %s", checks, err)
			return
		}
		con.w.WriteHeader(http.StatusSeeOther)
		return
	case "uncomment":
		_, err := DB.Exec(`update active_checks set notice = null where check_id = any($1::bigint[]);`,
			pq.Array(&checks))
		if err != nil {
			con.Error = "could not uncomment checks"
			returnError(http.StatusInternalServerError, con, con.w)
			log.Printf("could not uncomment checks: %s", err)
			return
		}
		con.w.WriteHeader(http.StatusSeeOther)
		return
	default:
		con.Error = fmt.Sprintf("requested action '%s' does not exist", action[0])
		returnError(http.StatusNotFound, con, con.w)
		return
	}
	whereColumn := "id"
	if setTable == "active_checks" || setTable == "checks_notify" {
		whereColumn = "check_id"
	}

	sql := "update " + setTable + " set " + setClause + " where " + whereColumn + " = any($1::bigint[])"
	if len(whereFields) > 0 {
		whereVals = append([]any{pq.Array(&checks)}, whereVals...)
		for i, column := range whereFields {
			sql = sql + " and " + column + fmt.Sprintf(" = $%d", i+1)
		}
	}

	_, err := DB.Exec(sql, whereVals)
	if err != nil {
		con.w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(con.w, "could not store changes")
		log.Printf("could not adjust checks %#v: %s", checks, err)
		return
	}
	con.w.WriteHeader(http.StatusSeeOther)
	return
}

func returnError(status int, con interface{}, w http.ResponseWriter) {
	w.Header()["Content-Type"] = []string{"text/html"}
	w.WriteHeader(status)
	if err := Tmpl.ExecuteTemplate(w, "error", con); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("problem with a template"))
		log.Printf("could not execute template: %s", err)
	}
}

func (c *Context) loadCommands() error {
	c.Commands = map[string]int{}
	rows, err := DB.Query(`select id, name from commands order by name`)
	if err != nil {
		return err
	}
	for rows.Next() {
		if rows.Err() != nil {
			return rows.Err()
		}
		var (
			id   int
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		c.Commands[name] = id
	}
	return nil
}

func (c *Context) loadMappings() error {
	c.Mappings = map[int]map[int]MapEntry{}
	rows, err := DB.Query(SQLShowMappings)
	if err != nil {
		return err
	}

	for rows.Next() {
		if rows.Err() != nil {
			return rows.Err()
		}
		var (
			mapId  int
			name   string
			target int
			title  string
			color  string
		)
		if err := rows.Scan(&mapId, &name, &target, &title, &color); err != nil {
			return err
		}
		ma, found := c.Mappings[mapId]
		if !found {
			ma = map[int]MapEntry{}
			c.Mappings[mapId] = ma
		}
		ma[target] = MapEntry{Title: title, Color: color, Name: name}
	}
	return nil
}

func showStatic(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/static/")
	raw, found := Static[file]
	if !found {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("file does not exist"))
		return
	}
	w.Header()["Content-Type"] = []string{"image/svg+xml"}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(raw))
	return
}

var (
	SQLShowMappings = `select mapping_id, name, target, title, color
	from mappings m join mapping_level ml on m.id = ml.mapping_id`
)

var (
	Templates = map[string]string{}
	Static    = map[string]string{
		"icon-mute":   `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 35.3 35.3" version="1.1"><title>Check is muted</title><style>.s0{fill:#191919;}</style><g transform="translate(0,-261.72223)"><path d="m17.6 261.7v35.3L5.3 284.7H0v-10.6l5.3 0zM30.2 273.1l-3.7 3.7-3.7-3.7-2.5 2.5 3.7 3.7-3.7 3.7 2.5 2.5 3.7-3.7 3.7 3.7 2.5-2.5-3.7-3.7 3.7-3.7z" fill="#191919"/></g></svg>`,
		"icon-notice": `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg" width="36" height="36"><path d="M2.572.19h30.857c1.319 0 2.38 1.356 2.38 3.041v19.98c0 1.685-1.061 3.04-2.38 3.04H15.941L4 35.81v-9.56H2.572C1.252 26.252.19 24.897.19 23.212V3.232C.19 1.545 1.252.19 2.57.19z" stroke="#000" stroke-width=".38" stroke-linejoin="round"/></svg>`,
		"error":       `{{ template "header" . }}{{ template "footer" . }}`,
	}
	TmplUnhandledGroups = `TODO`
	Funcs               = template.FuncMap{
		"int":       func(in int64) int { return int(in) },
		"sub":       func(base, amount int) int { return base - amount },
		"in":        func(t time.Time) time.Duration { return t.Sub(time.Now()).Round(1 * time.Second) },
		"since":     func(t time.Time) time.Duration { return time.Now().Sub(t).Round(1 * time.Second) },
		"now":       func() time.Time { return time.Now() },
		"join":      func(args []string, c string) string { return strings.Join(args, c) },
		"mapString": func(mapId, target int) string { return fmt.Sprintf("%d-%d", mapId, target) },
		"itoa":      func(i int) string { return strconv.Itoa(i) },
	}
)
