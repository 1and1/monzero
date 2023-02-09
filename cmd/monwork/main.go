package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"sync"
	"text/template"
	"time"

	"github.com/lib/pq"
)

var (
	configPath = flag.String("config", "monwork.conf", "path to the config file")
)

type (
	Config struct {
		DB            string `json:"db"`
		CheckInterval string `json:"interval"`
	}
)

func main() {
	flag.Parse()

	raw, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("could not read config: %s", err)
	}
	config := Config{}
	if err := json.Unmarshal(raw, &config); err != nil {
		log.Fatalf("could not parse config: %s", err)
	}

	checkInterval, err := time.ParseDuration(config.CheckInterval)
	if err != nil {
		log.Fatalf("could not parse check interval: %s", err)
	}

	db, err := sql.Open("postgres", config.DB)
	if err != nil {
		log.Fatalf("could not open database connection: %s", err)
	}

	go startNodeGen(db, checkInterval)
	go startCommandGen(db, checkInterval)
	go startConfigGen(db, checkInterval)

	// don't exit, we have work to do
	wg := sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}

func startNodeGen(db *sql.DB, checkInterval time.Duration) {
	for {
		tx, err := db.Begin()
		if err != nil {
			log.Printf("could not create transaction: %s", err)
			time.Sleep(checkInterval)
			continue
		}
		_, err = tx.Exec(`update checks c
		set updated = n.updated
		from nodes n
		where c.node_id = n.id
			and c.last_refresh < n.updated;`)
		if err != nil {
			log.Printf("could not update nodes: %s", err)
			tx.Rollback()
			time.Sleep(checkInterval)
			continue
		}
		if err := tx.Commit(); err != nil {
			log.Printf("could not commit node updates: %s", err)
			tx.Rollback()
		}
		time.Sleep(checkInterval)
	}
}

func startCommandGen(db *sql.DB, checkInterval time.Duration) {
	for {
		tx, err := db.Begin()
		if err != nil {
			log.Printf("could not create transaction: %s", err)
			time.Sleep(checkInterval)
			continue
		}
		_, err = tx.Exec(`update checks c
		set updated = co.updated
		from commands co
		where c.command_id = co.id
			and c.last_refresh < co.updated;`)
		if err != nil {
			log.Printf("could not update checks: %s", err)
			tx.Rollback()
			time.Sleep(checkInterval)
			continue
		}
		if err := tx.Commit(); err != nil {
			log.Printf("could not commit command updates: %s", err)
			tx.Rollback()
		}
		time.Sleep(checkInterval)
	}
}

func startConfigGen(db *sql.DB, checkInterval time.Duration) {
	for {
		tx, err := db.Begin()
		if err != nil {
			log.Printf("could not create transaction: %s", err)
			time.Sleep(checkInterval)
			continue
		}
		rows, err := tx.Query(SQLGetConfigUpdates)
		if err != nil {
			log.Printf("could not get updates: %s", err)
			tx.Rollback()
			time.Sleep(checkInterval)
			continue
		}
		var (
			check_id int
			command  string
			options  []byte
		)
		for rows.Next() {
			if rows.Err() != nil {
				log.Printf("could not receive rows: %s", err)
				break
			}
			if err := rows.Scan(&check_id, &command, &options); err != nil {
				log.Printf("could not scan row: %s", err)
				break
			}
		}
		if check_id == 0 {
			tx.Rollback()
			time.Sleep(checkInterval)
			continue
		}
		tmpl, err := template.New("command").Parse(command)
		if err != nil {
			tx.Rollback()
			log.Printf("could not parse command for check '%d': %s", check_id, err)
			time.Sleep(checkInterval)
			continue
		}
		var cmd bytes.Buffer
		var opts map[string]interface{}
		if err := json.Unmarshal(options, &opts); err != nil {
			tx.Rollback()
			log.Printf("could not parse options for check '%d': %s", check_id, err)
			time.Sleep(checkInterval)
			continue
		}
		if err := tmpl.Execute(&cmd, opts); err != nil {
			tx.Rollback()
			log.Printf("could not complete command for check '%d': %s", check_id, err)
			time.Sleep(checkInterval)
			continue
		}
		if _, err := tx.Exec(SQLRefreshActiveCheck, check_id, pq.Array(stringToShellFields(cmd.Bytes()))); err != nil {
			tx.Rollback()
			log.Printf("could not refresh check '%d': %s", check_id, err)
			continue
		}
		if _, err := tx.Exec(SQLUpdateLastRefresh, check_id); err != nil {
			tx.Rollback()
			log.Printf("could not update timestamp for check '%d': %s", check_id, err)
			continue
		}
		if err := tx.Commit(); err != nil {
			tx.Rollback()
			log.Printf("could not commit changes: %s", err)
		}
	}
}

func stringToShellFields(in []byte) [][]byte {
	if len(in) == 0 {
		return [][]byte{}
	}
	fields := bytes.Fields(in)
	result := [][]byte{}

	var quote byte

	for _, field := range fields {
		if quote == 0 && (field[0] != '\'' && field[0] != '"') {
			result = append(result, field)
			continue
		}
		if quote == 0 && (field[0] == '\'' || field[0] == '"') {
			quote = field[0]
			if field[len(field)-1] == quote {
				result = append(result, field[1:len(field)-1])
				quote = 0
				continue
			}
			result = append(result, field[1:])
			continue
		}
		idx := len(result) - 1
		if bytes.HasSuffix(field, []byte{quote}) {
			result[idx] = append(result[idx], append([]byte(" "), field[:len(field)-1]...)...)
			quote = 0
			continue
		}
		result[idx] = append(result[idx], append([]byte(" "), field...)...)
	}
	return result
}

var (
	SQLGetConfigUpdates = `select c.id, co.command, c.options
	from checks c
	join commands co on c.command_id = co.id
	where c.last_refresh < c.updated or c.last_refresh is null
  limit 1
	for update of c skip locked;`
	SQLRefreshActiveCheck = `insert into active_checks(check_id, cmdline, intval, enabled, msg, mapping_id, checker_id)
select c.id, $2, c.intval, c.enabled, case when ac.msg is null then '' else ac.msg end, case when c.mapping_id is not null then c.mapping_id when n.mapping_id is not null then n.mapping_id else 1 end, c.checker_id
from checks c
left join active_checks ac on c.id = ac.check_id
left join nodes n on c.node_id = n.id
where c.id = $1
on conflict(check_id)
do update set cmdline = $2, intval = excluded.intval, enabled = excluded.enabled, mapping_id = excluded.mapping_id, checker_id = excluded.checker_id;`
	SQLUpdateLastRefresh = `update checks set last_refresh = now() where id = $1;`
)
