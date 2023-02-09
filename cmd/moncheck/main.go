package main

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.zero-knowledge.org/gibheer/monzero"
)

var (
	configPath = flag.String("config", "moncheck.conf", "path to the config file")
)

type (
	Config struct {
		DB        string   `json:"db"`
		Timeout   string   `json:"timeout"`
		Wait      string   `json:"wait"`
		Path      []string `json:"path"`
		Workers   int      `json:"workers"`
		CheckerID int      `json:"checker_id"`
	}

	States []int
)

func main() {
	flag.Parse()

	raw, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Fatalf("could not read config: %s", err)
	}
	config := Config{Timeout: "30s", Wait: "30s", Workers: 25}
	if err := json.Unmarshal(raw, &config); err != nil {
		log.Fatalf("could not parse config: %s", err)
	}

	if err := os.Setenv("PATH", strings.Join(config.Path, ":")); err != nil {
		log.Fatalf("could not set PATH: %s", err)
	}

	waitDuration, err := time.ParseDuration(config.Wait)
	if err != nil {
		log.Fatalf("could not parse wait duration: %s", err)
	}
	timeout, err := time.ParseDuration(config.Timeout)
	if err != nil {
		log.Fatalf("could not parse timeout: %s", err)
	}

	db, err := sql.Open("postgres", config.DB)
	if err != nil {
		log.Fatalf("could not open database connection: %s", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("could not resolve hostname: %s", err)
	}

	checker, err := monzero.NewChecker(monzero.CheckerConfig{
		CheckerID:      config.CheckerID,
		DB:             db,
		Timeout:        timeout,
		HostIdentifier: hostname,
		Executor:       monzero.CheckExec,
	})
	if err != nil {
		log.Fatalf("could not create checker instance: %s", err)
	}

	for i := 0; i < config.Workers; i++ {
		go check(checker, waitDuration)
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	wg.Wait()
}

func check(checker *monzero.Checker, waitDuration time.Duration) {
	for {
		if err := checker.Next(); err != nil {
			if err != monzero.ErrNoCheck {
				log.Printf("could not run check: %s", err)
			}
			time.Sleep(waitDuration)
		}
	}
}

func (s *States) Value() (driver.Value, error) {
	last := len(*s)
	if last == 0 {
		return "{}", nil
	}
	result := strings.Builder{}
	_, err := result.WriteString("{")
	if err != nil {
		return "", fmt.Errorf("could not write to buffer: %s", err)
	}
	for i, state := range *s {
		if _, err := fmt.Fprintf(&result, "%d", state); err != nil {
			return "", fmt.Errorf("could not write to buffer: %s", err)
		}
		if i < last-1 {
			if _, err := result.WriteString(","); err != nil {
				return "", fmt.Errorf("could not write to buffer: %s", err)
			}
		}
	}
	if _, err := result.WriteString("}"); err != nil {
		return "", fmt.Errorf("could not write to buffer: %s", err)
	}
	return result.String(), nil
}

func (s *States) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		tmp := bytes.Trim(src, "{}")
		states := bytes.Split(tmp, []byte(","))
		result := make([]int, len(states))
		for i, state := range states {
			var err error
			result[i], err = strconv.Atoi(string(state))
			if err != nil {
				return fmt.Errorf("could not parse element %s: %s", state, err)
			}
		}
		*s = result
		return nil
	default:
		return fmt.Errorf("could not convert %T to states", src)
	}
}

// Append prepends the new state before all others.
func (s *States) Add(state int) {
	vals := *s
	statePos := 5
	if len(vals) < 6 {
		statePos = len(vals)
	}
	*s = append([]int{state}, vals[:statePos]...)
	return
}

// ToOK returns true when the state returns from != 0 to 0.
func (s *States) ToOK() bool {
	vals := *s
	if len(vals) == 0 {
		return false
	}
	if len(vals) <= 1 {
		return vals[0] == 0
	}
	if vals[0] == 0 && vals[1] > 0 {
		return true
	}
	return false
}
