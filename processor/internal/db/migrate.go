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

// DropForeignKeys removes all FK constraints referencing humans(id).
// Tracking table cleanup is handled by application code instead.
// Safe to call repeatedly — silently skips constraints that don't exist.
func DropForeignKeys(db *sql.DB) {
	fks := []struct{ table, constraint string }{
		{"profiles", "profiles_id_foreign"},
		{"monsters", "monsters_id_foreign"},
		{"raid", "raid_id_foreign"},
		{"egg", "egg_id_foreign"},
		{"quest", "quest_id_foreign"},
		{"invasion", "invasion_id_foreign"},
		{"lures", "lures_id_foreign"},
		{"nests", "nests_id_foreign"},
		{"gym", "gym_id_foreign"},
		{"forts", "forts_id_foreign"},
		{"weather", "weather_id_foreign"},
		{"maxbattle", "maxbattle_id_foreign"},
	}

	for _, fk := range fks {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND CONSTRAINT_NAME = ? AND CONSTRAINT_TYPE = 'FOREIGN KEY'`,
			fk.table, fk.constraint).Scan(&count)
		if err != nil || count == 0 {
			continue
		}
		_, err = db.Exec(fmt.Sprintf("ALTER TABLE `%s` DROP FOREIGN KEY `%s`", fk.table, fk.constraint))
		if err != nil {
			log.Warnf("Could not drop FK %s.%s: %s", fk.table, fk.constraint, err)
		} else {
			log.Infof("Dropped foreign key %s.%s", fk.table, fk.constraint)
		}
	}
}

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

	return nil
}
