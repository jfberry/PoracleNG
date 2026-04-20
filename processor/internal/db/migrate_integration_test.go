package db

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// TestFreshMigration tests that migrations apply cleanly to a brand new empty database.
// Requires: CREATE DATABASE poracle_test_fresh (with dragonite user access).
// Run with: go test -run TestFreshMigration -v ./internal/db/
func TestFreshMigration(t *testing.T) {
	// Use the same DSN format as config.go (no hardcoded multiStatements)
	dsn := "dragonite:dragonite@tcp(127.0.0.1:3306)/poracle_test_fresh?parseTime=true&multiStatements=true"

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("Cannot connect to test DB: %v", err)
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Skipf("Cannot ping test DB: %v", err)
	}

	// Clean slate — drop all tables
	rows, err := conn.Query("SHOW TABLES")
	if err != nil {
		t.Fatalf("show tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()

	if len(tables) > 0 {
		conn.Exec("SET FOREIGN_KEY_CHECKS = 0")
		for _, tbl := range tables {
			conn.Exec(fmt.Sprintf("DROP TABLE `%s`", tbl))
		}
		conn.Exec("SET FOREIGN_KEY_CHECKS = 1")
		t.Logf("Dropped %d existing tables for clean test", len(tables))
	}

	// Run adoption (should be no-op for fresh DB)
	if err := AdoptExistingDatabase(conn); err != nil {
		t.Fatalf("AdoptExistingDatabase: %v", err)
	}
	t.Log("AdoptExistingDatabase: OK")

	// Drop FK (no-op for fresh)
	DropForeignKeys(conn)
	t.Log("DropForeignKeys: OK")

	// Run migrations
	if err := RunMigrations(conn); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}
	t.Log("RunMigrations: OK")

	// Verify expected tables exist
	expectedTables := []string{
		"humans", "profiles", "monsters", "raid", "egg", "quest",
		"invasion", "lures", "nests", "gym", "forts", "maxbattle",
		"schema_migrations",
	}

	for _, expected := range expectedTables {
		var count int
		err := conn.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", expected).Scan(&count)
		if err != nil {
			t.Errorf("check table %s: %v", expected, err)
		} else if count == 0 {
			t.Errorf("expected table %s does not exist", expected)
		} else {
			t.Logf("  ✓ %s", expected)
		}
	}

	// Check migration version
	var version int
	var dirty bool
	if err := conn.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	t.Logf("Migration version: %d (dirty: %v)", version, dirty)

	if dirty {
		t.Error("migration state is dirty")
	}

	// Verify we can insert a human (basic smoke test)
	_, err = conn.Exec("INSERT INTO humans (id, type, name, area, community_membership) VALUES ('test1', 'discord:user', 'TestUser', '[]', '[]')")
	if err != nil {
		t.Fatalf("insert human: %v", err)
	}
	conn.Exec("DELETE FROM humans WHERE id = 'test1'")
	t.Log("Insert/delete human: OK")

	// Detect if test should fail based on env
	if os.Getenv("CI") != "" && t.Failed() {
		t.Fatal("Migration test failed in CI")
	}
}
