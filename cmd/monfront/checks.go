package main

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/lib/pq"
)

type (
	check struct {
		NodeId      int
		NodeName    string
		CommandName string
		CheckID     int64
		CheckName   string
		MappingId   int
		State       int
		Enabled     bool
		Notify      bool
		Notice      sql.NullString
		NextTime    time.Time
		Msg         string
		StateSince  time.Time
	}

	checkDetails struct {
		Id             int64
		Name           string
		Message        string
		Enabled        bool
		Updated        time.Time
		LastRefresh    time.Time
		NextTime       time.Time
		MappingId      int
		MappingName    string
		NodeId         int
		NodeName       string
		NodeMessage    string
		CommandId      int
		CommandName    string
		CommandLine    []string
		CommandMessage string
		States         []int64
		Notice         sql.NullString
		Notifiers      []notifier
		Notifications  []notification
		CheckerID      int
		CheckerName    string
		CheckerMsg     string
	}

	notifier struct {
		Id      int
		Name    string
		Enabled bool
	}

	notification struct {
		Id           int64
		State        int
		Output       string
		Inserted     time.Time
		Sent         pq.NullTime
		NotifierName string
		MappingId    int
	}
)

// showCheck loads shows the notifications for a specific check.
func showCheck(con *Context) {
	cd := checkDetails{}
	con.CheckDetails = &cd
	id, found := con.r.URL.Query()["check_id"]
	if !found {
		con.Error = "no check given to view"
		returnError(http.StatusNotFound, con, con.w)
		return
	}
	query := `select c.id, c.name, c.message, c.enabled, c.updated, c.last_refresh,
		m.id, m.name, n.id, n.name, n.message, co.id, co.Name, co.message,
		ac.cmdline, ac.states, ac.msg, ac.next_time, ch.id, ch.name, ch.description
	from checks c
	join active_checks ac on c.id = ac.check_id
	join nodes n on c.node_id = n.id
	join commands co on c.command_id = co.id
	join mappings m on ac.mapping_id = m.id
	join checkers ch on c.checker_id = ch.id
	where c.id = $1::bigint`
	err := DB.QueryRow(query, id[0]).Scan(&cd.Id, &cd.Name, &cd.Message, &cd.Enabled,
		&cd.Updated, &cd.LastRefresh, &cd.MappingId, &cd.MappingName, &cd.NodeId,
		&cd.NodeName, &cd.NodeMessage, &cd.CommandId, &cd.CommandName, &cd.CommandMessage,
		pq.Array(&cd.CommandLine), pq.Array(&cd.States), &cd.Notice, &cd.NextTime,
		&cd.CheckerID, &cd.CheckerName, &cd.CheckerMsg)
	if err != nil && err == sql.ErrNoRows {
		con.w.Header()["Location"] = []string{"/"}
		con.w.WriteHeader(http.StatusSeeOther)
		return
	} else if err != nil {
		con.w.WriteHeader(http.StatusInternalServerError)
		con.w.Write([]byte("problems with the database"))
		log.Printf("could not get check details for check id %s: %s", id[0], err)
		return
	}

	query = `select n.id, states[1], output, inserted, sent, no.name, n.mapping_id
		from notifications n
		join notifier no on n.notifier_id = no.id
		where check_id = $1::bigint
		order by inserted desc
		limit 500`
	rows, err := DB.Query(query, cd.Id)
	defer rows.Close()
	if err != nil {
		log.Printf("could not load notifications: %s", err)
		con.Error = "could not load notification information"
		returnError(http.StatusInternalServerError, con, con.w)
		return
	}
	cd.Notifications = []notification{}
	for rows.Next() {
		if err := rows.Err(); err != nil {
			log.Printf("could not load notifications: %s", err)
			con.Error = "could not load notification information"
			returnError(http.StatusInternalServerError, con, con.w)
			return
		}
		no := notification{}
		if err := rows.Scan(&no.Id, &no.State, &no.Output, &no.Inserted,
			&no.Sent, &no.NotifierName, &no.MappingId); err != nil {
			log.Printf("could not scan notifications: %s", err)
			con.Error = "could not load notification information"
			returnError(http.StatusInternalServerError, con, con.w)
			return
		}
		cd.Notifications = append(cd.Notifications, no)
	}

	if err := con.loadMappings(); err != nil {
		con.w.WriteHeader(http.StatusInternalServerError)
		con.w.Write([]byte("problem with the mappings"))
		log.Printf("could not load mappings: %s", err)
		return
	}

	con.w.Header()["Content-Type"] = []string{"text/html"}
	con.Render("check")
}

func showChecks(con *Context) {
	query := `select c.id, c.name, n.id, n.name, co.name, ac.mapping_id, ac.states[1] as state,
	ac.enabled, ac.notice, ac.next_time, ac.msg,
	case when cn.check_id is null then false else true end as notify_enabled,
	state_since
  from active_checks ac
	join checks c on ac.check_id = c.id
	join nodes n on c.node_id = n.id
	join commands co on c.command_id = co.id
	left join ( select distinct check_id from checks_notify where enabled = true) cn on c.id = cn.check_id`
	filter := newFilter()
	con.Filter = filter
	if id, found := con.r.URL.Query()["group_id"]; found {
		query += ` join nodes_groups ng on n.id = ng.node_id`
		filter.Add("ng.group_id", "=", id[0], "int")
	}
	filter.filterChecks(con)
	if search, found := con.r.URL.Query()["search"]; found {
		filter.AddSpecial(
			`to_tsvector('english', regexp_replace(n.name, '[.-/]', ' ', 'g'))`,
			`@@`,
			`to_tsquery('english', regexp_replace($%d, '[.-/]', ' & ', 'g') || ':*')`,
			search[0])
	}
	if id, found := con.r.URL.Query()["node_id"]; found {
		filter.Add("n.id", "=", id[0], "int")
	}
	if id, found := con.r.URL.Query()["check_id"]; found {
		filter.Add("c.id", "=", id[0], "int")
	}
	where, params := filter.Join()
	if len(where) > 0 {
		query += " where " + where
	}
	query += ` order by n.name, c.name, co.name`
	rows, err := DB.Query(query, params...)
	if err != nil {
		con.w.WriteHeader(http.StatusInternalServerError)
		con.w.Write([]byte("problems with the database"))
		log.Printf("could not get check list: %s", err)
		return
	}
	defer rows.Close()

	checks := []check{}
	for rows.Next() {
		c := check{}
		err := rows.Scan(&c.CheckID, &c.CheckName, &c.NodeId, &c.NodeName, &c.CommandName, &c.MappingId,
			&c.State, &c.Enabled, &c.Notice, &c.NextTime, &c.Msg, &c.Notify, &c.StateSince)
		if err != nil {
			con.w.WriteHeader(http.StatusInternalServerError)
			returnError(http.StatusInternalServerError, con, con.w)
			log.Printf("could not get check list: %s", err)
			return
		}
		checks = append(checks, c)
	}
	con.Checks = checks
	if err := con.loadCommands(); err != nil {
		con.Error = "could not load commands"
		returnError(http.StatusInternalServerError, con, con.w)
		log.Printf("could not get commands: %s", err)
		return
	}
	if err := con.loadMappings(); err != nil {
		con.Error = "could not load mapping data"
		con.w.WriteHeader(http.StatusInternalServerError)
		con.w.Write([]byte("problem with the mappings"))
		log.Printf("could not load mappings: %s", err)
		return
	}
	con.w.Header()["Content-Type"] = []string{"text/html"}
	con.Render("checklist")
	return
}
