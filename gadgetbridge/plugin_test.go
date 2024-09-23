package gadgetbridge

import (
	"database/sql"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "embed"

	"github.com/alecthomas/assert/v2"
	"github.com/hexops/autogold/v2"
	telegraftest "github.com/influxdata/telegraf/testutil"
)

//go:embed testdata/gadgetbridge.sql
var gadgetbridgeDump string

func TestPlugin_Gather(t *testing.T) {
	dbPath := newTestDB(t, gadgetbridgeDump)

	var state any

	t.Run("pass 1", func(t *testing.T) {
		p := &Plugin{DatabasePaths: []string{dbPath}}
		assert.NoError(t, p.Init())

		assert.NoError(t, p.SetState(state))
		t.Cleanup(func() { state = p.GetState() })

		acc := new(telegraftest.Accumulator)
		acc.TimeFunc = randomTime

		assert.NoError(t, p.Gather(acc))

		for _, metric := range acc.Metrics {
			// Manually verify the 'database_path' tag.
			assert.Equal(t, dbPath, metric.Tags["database_path"], "database_path tag mismatch")
			delete(metric.Tags, "database_path")

			// Manually verify the timestamp.
			assert.False(t, metric.Time.IsZero(), "metric timestamp is zero")
		}

		autogold.ExpectFile(t, acc.Metrics, autogold.Name("TestPlugin_Gather/metrics"))
	})

	t.Run("pass 2", func(t *testing.T) {
		p := &Plugin{DatabasePaths: []string{dbPath}}
		assert.NoError(t, p.Init())
		assert.NoError(t, p.SetState(state))

		acc2 := new(telegraftest.Accumulator)
		assert.NoError(t, p.Gather(acc2))

		if len(acc2.Metrics) > 0 {
			t.Errorf("unexpected %d metrics gathered on second pass", len(acc2.Metrics))
			for _, metric := range acc2.Metrics {
				t.Logf("  %s %s", metric.Time, metric.Measurement)
			}
		}

		assert.Equal(t, state, p.GetState(), "state mismatch")
	})
}

func newTestDB(t *testing.T, sqlDump string) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gadgetbridge-test")
	assert.NoError(t, err, "failed to create temp dir")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "gadgetbridge.db")

	db, err := sql.Open("sqlite", dbPath)
	assert.NoError(t, err, "failed to open SQLite database")
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(sqlDump)
	assert.NoError(t, err, "failed to apply SQLite database dump")

	return dbPath
}

func randomTime() time.Time { return time.Unix(rand.Int63(), 0) }
