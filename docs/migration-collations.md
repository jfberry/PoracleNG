# Mixed collations during migration

Legacy Knex databases skip migration `000001`, including its database-default
collation setting. Older tables can use `utf8mb4_unicode_ci` (or another
collation, or `utf8mb3`) while tables created later inherit a different
default, such as MySQL 8's `utf8mb4_0900_ai_ci`. Changing a database default
does not change existing columns.

The original migration `000009` (fold the accidental profile 0 into profile 1)
compared user IDs across tables directly, so on such databases it failed with
`Illegal mix of collations for operation 'UNION'`. This does not involve
foreign keys; user deletion remains application-managed.

## The fix

- `000009` now normalises IDs to `utf8mb4_unicode_ci` in temporary tables and
  converts only the persistent table's side of each comparison, so every
  lookup uses a temporary table's primary key. It does not alter persistent
  columns. The temporary tables declare their `id` column explicitly
  (`VARCHAR(255)`, `utf8mb4_unicode_ci`): MariaDB 10.5 and 10.6 type a
  `CONVERT(… USING utf8mb4)` result as TEXT in `CREATE … SELECT`, and a TEXT
  column cannot be a primary key without a prefix length.
- All of its data changes commit in one transaction, so a failure part-way
  leaves the data unchanged (InnoDB tables; a MyISAM table would not roll
  back). It is idempotent: once it succeeds nothing is left on a fake
  profile 0.
- Migrations `000002`–`000004` now create their tables with
  `utf8mb4_unicode_ci` instead of inheriting an installation-dependent default.

Databases where the original `000009` succeeded need nothing further.

## Dirty migrations

Startup no longer forces a dirty migration clean and advances past it: a
failed multi-statement migration may be only partly applied. A dirty `000009`
is retried automatically, as it is safe to re-run. Any other dirty version
stops startup with an error.

To recover from another dirty version, stop the application, take a backup,
read the original error and inspect `schema_migrations` (`version`, `dirty`).
Either restore a backup from before that migration, or finish/undo its
statements by hand and then set `schema_migrations` to the version you have
actually reached with `dirty = 0`.

## If 000009 failed and an older build skipped it

Older builds forced a failed migration clean on the next start, so a database
where `000009` failed and the processor was then restarted records it as
applied without having run it. Humans created on profile 0 are then still on
it. To run the corrected migration, stop the processor and set
`schema_migrations` back to version 8 with `dirty = 0`; `000009` and any later
migrations then run on the next start. This is safe only because `000009` is
idempotent; do not reset past any other migration this way.

## Regression tests

The tests in `processor/internal/db/migrate_collation_test.go` need
`PORACLENG_MIGRATION_TEST_DSN` pointing at MySQL 8 or MariaDB, with permission
to create and drop databases. Each creates and removes its own uniquely named
database. They cover mixed collations across all tracking tables, re-running
`000009`, rollback after a failure part-way, and dirty-state handling. They
pass on MySQL 8.0 and 8.4 and MariaDB 10.5, 10.6, 10.11 and 11.4.
