// Package gadgetbridge implements a Telegraf plugin that ingests data from
// Gadgetbridge's auto-export file and sends it to Telegraf.
package gadgetbridge

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	_ "embed"

	"github.com/doug-martin/goqu/v9"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"

	_ "github.com/doug-martin/goqu/v9/dialect/sqlite3"
	_ "modernc.org/sqlite"
)

func init() {
	inputs.Add("gadgetbridge", func() telegraf.Input { return &Plugin{} })
}

//go:embed config.example.toml
var sampleConfig string

// Plugin implements the Telegraf input plugin.
type Plugin struct {
	DatabasePaths []string           `toml:"database_paths"`
	ExtraTables   []TableDescription `toml:"extra_tables,omitempty"`

	mu    sync.Mutex
	state pluginState
}

type pluginState struct {
	// LastTableTimes is a map of the last timestamp for each table that was
	// read. Typically, this tracks the `TIMESTAMP` column for certain tables
	// that are read periodically.
	LastTableTimes map[string]int64 `json:"last_table_times"`
}

var (
	_ telegraf.Input          = (*Plugin)(nil)
	_ telegraf.Initializer    = (*Plugin)(nil)
	_ telegraf.StatefulPlugin = (*Plugin)(nil)
)

func (p *Plugin) SampleConfig() string {
	return sampleConfig
}

func (p *Plugin) Init() error {
	p.SetState(nil)
	return nil
}

// TableDescription describes a table in the database.
// It is used to determine which tables to read and how to parse the data.
type TableDescription struct {
	// Name is the name of the table in the database.
	Name string `toml:"table"`
	// Columns describes the columns in the table.
	Columns TableColumns `toml:"columns"`
}

// TableColumns describes the columns in a table.
type TableColumns struct {
	// Timestamp is the name of the column that contains the timestamp.
	// This must not be empty.
	Timestamp string `toml:"timestamp"`
	// Tags is a list of columns that contain the tags to be parsed as strings.
	Tags []string `toml:"tags"`
	// Fields is a list of columns that contain the fields to be parsed
	// numerically (as either int64 or float64).
	Fields []string `toml:"fields"`
}

var knownTables = []TableDescription{
	{
		Name: "HYBRID_HRACTIVITY_SAMPLE",
		Columns: TableColumns{
			Timestamp: "TIMESTAMP",
			Tags:      []string{"USER_ID", "DEVICE_ID"},
			Fields:    []string{"WEAR_TYPE", "STEPS", "CALORIES", "VARIABILITY", "MAX_VARIABILITY", "HEARTRATE_QUALITY", "ACTIVE", "HEART_RATE"},
		},
	},
	{
		Name: "BATTERY_LEVEL",
		Columns: TableColumns{
			Timestamp: "TIMESTAMP",
			Tags:      []string{"DEVICE_ID", "BATTERY_INDEX"},
			Fields:    []string{"LEVEL"},
		},
	},
}

func openDB(path string) (*sql.DB, error) {
	connURI := url.URL{
		Scheme: "file",
		Path:   path,
	}

	connQuery := connURI.Query()
	connQuery.Set("mode", "ro")
	connQuery.Set("immutable", "1")
	connURI.RawQuery = connQuery.Encode()

	db, err := sql.Open("sqlite", connURI.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Prevent concurrent access to the database as SQLite doesn't support it.
	db.SetMaxOpenConns(1)

	return db, nil
}

func (p *Plugin) Gather(acc telegraf.Accumulator) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error

	for _, path := range p.DatabasePaths {
		db, err := openDB(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to open database %q: %w", path, err))
			continue
		}

		for _, t := range slices.Concat(knownTables, p.ExtraTables) {
			if err := p.gatherTable(acc, db, path, t); err != nil {
				errs = append(errs, fmt.Errorf("error at table %q: %w", t.Name, err))
			}
		}

		if err := db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close database %q: %w", path, err))
		}
	}

	return errors.Join(errs...)
}

var sqliteBuilder = goqu.Dialect("sqlite")

func (p *Plugin) gatherTable(acc telegraf.Accumulator, db *sql.DB, dbPath string, t TableDescription) error {
	q := sqliteBuilder.
		From(t.Name).
		Select(sliceAny(slices.Concat(
			[]string{t.Columns.Timestamp},
			t.Columns.Tags,
			t.Columns.Fields,
		))...).
		Order(goqu.C(t.Columns.Timestamp).Asc())
	if lastTime, ok := p.state.LastTableTimes[t.Name]; ok {
		q = q.Where(goqu.C(t.Columns.Timestamp).Gt(lastTime))
	}

	qSQL, qArgs, err := q.ToSQL()
	if err != nil {
		return fmt.Errorf("error building query: %w", err)
	}

	r, err := db.Query(qSQL, qArgs...)
	if err != nil {
		return err
	}
	defer r.Close()

	tags := make(map[string]string, len(t.Columns.Tags))
	tags["database_path"] = dbPath
	fields := make(map[string]interface{}, len(t.Columns.Fields))

	tagOffset := 1
	fieldOffset := tagOffset + len(t.Columns.Tags)

	var ts int64
	v := slices.Concat(
		[]any{&ts},
		sliceOfPointers[string](len(t.Columns.Tags)),
		sliceOfPointers[any](len(t.Columns.Fields)),
	)

	for r.Next() {
		if err := r.Scan(v...); err != nil {
			return fmt.Errorf("error scanning row: %w", err)
		}

		for i, tag := range t.Columns.Tags {
			v := *v[tagOffset+i].(*string)
			tags[strings.ToLower(tag)] = v
		}

		for i, field := range t.Columns.Fields {
			v := *v[fieldOffset+i].(*any)
			fields[strings.ToLower(field)] = v
		}

		acc.AddFields(strings.ToLower(t.Name), fields, tags, time.Unix(ts, 0))
		p.state.LastTableTimes[t.Name] = ts
	}

	if err := r.Err(); err != nil {
		return fmt.Errorf("error reading rows: %w", err)
	}

	return nil
}

func sliceAny[T1 any](s []T1) []any {
	r := make([]any, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}

func sliceOfPointers[T any](n int) []any {
	s := make([]any, n)
	for i := range s {
		s[i] = new(T)
	}
	return s
}

func (p *Plugin) GetState() interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()

	return pluginState{
		LastTableTimes: maps.Clone(p.state.LastTableTimes),
	}
}

func (p *Plugin) SetState(state interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch state := state.(type) {
	case nil:
		p.state = pluginState{
			LastTableTimes: make(map[string]int64),
		}
	case pluginState:
		p.state = state
	default:
		return fmt.Errorf("invalid state type: %T", state)
	}

	return nil
}
