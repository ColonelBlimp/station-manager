package sqlite_test

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	sqlite "github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// CC-3 parity. Decision (b): config.Validate and the SQLite consumer validator are
// SEPARATE implementations of the same datastore rules; this test guards them against
// drift by asserting they reach the SAME verdict (reject / accept) for every input. A
// rule added to one but not the other — or a boundary that disagrees — fails here.
func TestDatastoreValidationParity(t *testing.T) {
	valid := config.DefaultConfig(t.TempDir()).Datastore
	vary := func(f func(d *types.DatastoreConfig)) types.DatastoreConfig {
		d := valid
		f(&d)
		return d
	}
	cases := []struct {
		name string
		ds   types.DatastoreConfig
	}{
		{"default valid", valid},
		{"driver postgres", vary(func(d *types.DatastoreConfig) { d.Driver = "postgres" })},
		{"driver empty", vary(func(d *types.DatastoreConfig) { d.Driver = "" })},
		{"path empty", vary(func(d *types.DatastoreConfig) { d.Path = "" })},
		{"max_open_conns 0", vary(func(d *types.DatastoreConfig) { d.MaxOpenConns = 0 })},
		{"max_open_conns 1", vary(func(d *types.DatastoreConfig) { d.MaxOpenConns = 1 })},
		{"max_idle_conns 0", vary(func(d *types.DatastoreConfig) { d.MaxIdleConns = 0 })},
		{"conn_max_lifetime -1", vary(func(d *types.DatastoreConfig) { d.ConnMaxLifetime = -1 })},
		{"conn_max_lifetime 0", vary(func(d *types.DatastoreConfig) { d.ConnMaxLifetime = 0 })},
		{"conn_max_idle_time -1", vary(func(d *types.DatastoreConfig) { d.ConnMaxIdleTime = -1 })},
		{"context_timeout 4", vary(func(d *types.DatastoreConfig) { d.ContextTimeout = 4 })},
		{"context_timeout 5", vary(func(d *types.DatastoreConfig) { d.ContextTimeout = 5 })},
		{"transaction_context_timeout 4", vary(func(d *types.DatastoreConfig) { d.TransactionContextTimeout = 4 })},
		{"transaction_context_timeout 5", vary(func(d *types.DatastoreConfig) { d.TransactionContextTimeout = 5 })},
	}
	base := config.DefaultConfig(t.TempDir())
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := base
			cfg.Datastore = c.ds
			configRejects := false
			for _, f := range config.Validate(cfg) {
				if f.Code == "invalid_datastore" {
					configRejects = true
					break
				}
			}
			ds := c.ds
			consumerRejects := sqlite.ValidateDatastoreConfig(&ds) != nil
			if configRejects != consumerRejects {
				t.Errorf("parity mismatch: config.Validate rejects=%v, sqlite consumer rejects=%v",
					configRejects, consumerRejects)
			}
		})
	}
}
