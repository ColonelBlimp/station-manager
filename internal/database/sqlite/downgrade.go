package sqlite

import (
	stderr "errors"

	"github.com/golang-migrate/migrate/v4"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// DowngradeLogSchemaTo migrates the LOG migration set down to `target` and
// returns the version it started from. It is the operator's rollback path
// (`smd db-downgrade`, W-0021 slice 1): a tagged older binary cannot open a file
// whose schema version is absent from its bundled migration source (golang-migrate
// requires the current version to exist before it reads further), so returning to
// an older build means first returning the file to that build's head.
//
// Down only — the daemon is what migrates up — and log set only: reference.db is
// station-global and never part of a rollback. A target at or above the current
// version is refused before any migration runs. Every migration in the set ships
// a `.down.sql`, so the retained rows are whatever each down step preserves
// (the drill proves that on copies before a schema commit lands). The daemon
// must be stopped: this takes the same connection settings and runs against the
// live file.
func (s *Service) DowngradeLogSchemaTo(target uint) (from uint, err error) {
	const op errors.Op = "sqlite.Service.DowngradeLogSchemaTo"
	if err := checkService(op, s); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle == nil || !s.isOpen.Load() {
		return 0, errors.New(op).WithMsg(errMsgNotOpen)
	}

	srcDriver, dbDriver, err := GetMigrationDrivers(s.handle, MigrationSetLog)
	if err != nil {
		return 0, errors.New(op).WithErr(err)
	}
	defer func() { _ = srcDriver.Close() }()

	m, err := migrate.NewWithInstance("iofs", srcDriver, s.DatabaseConfig.Driver, dbDriver)
	if err != nil {
		return 0, errors.New(op).WithErr(err).WithMsg("migrate.NewWithInstance")
	}
	current, dirty, err := m.Version()
	if err != nil && !stderr.Is(err, migrate.ErrNilVersion) {
		return 0, errors.New(op).WithErr(err).WithMsg("read schema version")
	}
	if dirty {
		return current, errors.New(op).WithMsgf("schema version %d is dirty; repair it before a downgrade", current)
	}
	if target < 1 {
		// Version 0 is no schema at all: 0001's down step drops every table. No
		// build ever ran there, so it is never a rollback target (review b0d94f13).
		return current, errors.New(op).WithMsg("target version must be at least 1 (version 0 would drop every table)")
	}
	if target >= current {
		return current, errors.New(op).WithMsgf("target version %d is not below the current version %d (down only)", target, current)
	}
	if err := m.Migrate(target); err != nil {
		return current, errors.New(op).WithErr(err).WithMsgf("migrate log schema %d → %d", current, target)
	}
	return current, nil
}
