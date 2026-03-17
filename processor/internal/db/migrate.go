package db

import (
	"database/sql"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/db/migrations"
)

// RunMigrations runs all pending database migrations.
func RunMigrations(db *sql.DB) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}

	driver, err := mysql.WithInstance(db, &mysql.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("get migration version: %w", err)
	}

	if dirty {
		log.Warnf("Database migration state is dirty at version %d, attempting force", version)
		if err := m.Force(int(version)); err != nil {
			return fmt.Errorf("force migration version: %w", err)
		}
	}

	if err == migrate.ErrNilVersion {
		log.Info("No migration history found, running all migrations")
	} else {
		log.Infof("Database at migration version %d", version)
	}

	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			log.Info("Database schema is up to date")
			return nil
		}
		return fmt.Errorf("run migrations: %w", err)
	}

	newVersion, _, _ := m.Version()
	log.Infof("Database migrated to version %d", newVersion)
	return nil
}

// AdoptExistingDatabase creates the schema_migrations table for an existing
// Knex-managed database and marks the initial migration as already applied.
// This allows golang-migrate to take over without re-running the initial schema.
func AdoptExistingDatabase(db *sql.DB) error {
	// Check if schema_migrations already exists (already adopted)
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'schema_migrations'").Scan(&count)
	if err != nil {
		return fmt.Errorf("check schema_migrations: %w", err)
	}
	if count > 0 {
		return nil // already adopted
	}

	// Check if this is actually a Knex database (has the humans table)
	err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'humans'").Scan(&count)
	if err != nil {
		return fmt.Errorf("check humans table: %w", err)
	}
	if count == 0 {
		return nil // fresh database, let migrations create everything
	}

	// Existing Knex database — create schema_migrations and mark version 1 as done
	log.Info("Adopting existing Knex database for golang-migrate")

	_, err = db.Exec(`CREATE TABLE schema_migrations (
		version bigint NOT NULL,
		dirty boolean NOT NULL,
		PRIMARY KEY (version)
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	_, err = db.Exec("INSERT INTO schema_migrations (version, dirty) VALUES (1, false)")
	if err != nil {
		return fmt.Errorf("set initial version: %w", err)
	}

	log.Info("Database adopted: schema_migrations created at version 1 (initial schema skipped)")

	// TODO: Clean up Knex migration tables once migration is stable
	// _, _ = db.Exec("DROP TABLE IF EXISTS migrations_lock")
	// _, _ = db.Exec("DROP TABLE IF EXISTS migrations")

	return nil
}
