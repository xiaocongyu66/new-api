package dbinfra

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/common"
)

//go:embed migrations/*.sql
var sqlMigrations embed.FS

// sqlMigration is one versioned SQL file from db/migrations (embedded from
// internal/dbinfra/migrations). Name format: NNN_description.sql, applied in
// name order.
type sqlMigration struct {
	name string
	sql  string
}

// listSQLMigrations returns the embedded files sorted by name.
func listSQLMigrations() ([]sqlMigration, error) {
	entries, err := sqlMigrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []sqlMigration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := sqlMigrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, sqlMigration{name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

const sqlMigrationsBookkeeping = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    varchar(255) PRIMARY KEY,
	applied_at bigint NOT NULL
)`

// migrationAppliesTo reports whether a file should run on the given database.
// A header comment `-- applies-to: postgres` restricts it; absent or
// `-- applies-to: all` means every backend. The tag is matched against the
// common.DatabaseType string (sqlite, mysql, postgresql, clickhouse).
func migrationAppliesTo(sql string, dbType string) (bool, error) {
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			break // header comments end at the first non-comment line
		}
		if !strings.HasPrefix(trimmed, "-- applies-to:") {
			continue
		}
		tag := strings.TrimSpace(strings.TrimPrefix(trimmed, "-- applies-to:"))
		if tag == "all" {
			return true, nil
		}
		for _, t := range strings.Split(tag, ",") {
			if strings.EqualFold(strings.TrimSpace(t), dbType) {
				return true, nil
			}
		}
		return false, nil
	}
	return true, nil
}

// RunSQLMigrations applies each embedded migration not yet recorded in
// schema_migrations, in filename order, one transaction per file. A failure
// aborts startup: a half-applied sequence would make the recorded state lie.
func RunSQLMigrations(gdb *gorm.DB, dbType string) error {
	if gdb == nil {
		return nil
	}
	migrations, err := listSQLMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return nil
	}

	if err := gdb.Exec(sqlMigrationsBookkeeping).Error; err != nil {
		return fmt.Errorf("schema_migrations bookkeeping: %w", err)
	}

	for _, m := range migrations {
		apply, err := migrationAppliesTo(m.sql, dbType)
		if err != nil {
			return err
		}
		if !apply {
			continue
		}

		// Any statement in the file may itself be conditional (IF NOT EXISTS,
		// IF EXISTS), so re-running an already-applied file must be harmless.
		applied := int64(0)
		if err := gdb.Raw("SELECT count(*) FROM schema_migrations WHERE version = ?", m.name).
			Scan(&applied).Error; err != nil {
			return fmt.Errorf("schema_migrations lookup %s: %w", m.name, err)
		}
		if applied > 0 {
			continue
		}

		err = gdb.Transaction(func(tx *gorm.DB) error {
			for _, stmt := range splitSQLStatements(m.sql) {
				if stmt == "" {
					continue
				}
				if err := tx.Exec(stmt).Error; err != nil {
					return fmt.Errorf("%s: %w", m.name, err)
				}
			}
			return tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
				m.name, time.Now().Unix()).Error
		})
		if err != nil {
			return err
		}
		common.SysLog(fmt.Sprintf("schema migration applied: %s", m.name))
	}
	return nil
}

// splitSQLStatements splits a script on semicolons that terminate a statement.
// It skips over single-quoted strings, line comments, and dollar-quoted bodies
// ($$...$$ and $tag$...$tag$), so DO blocks with nested dollar quotes survive
// intact. The files this runs are project-controlled; anything needing more
// grammar than that should be registered in Go instead.
func splitSQLStatements(script string) []string {
	var out []string
	var sb strings.Builder
	flush := func() {
		if s := strings.TrimSpace(sb.String()); s != "" {
			out = append(out, s)
		}
		sb.Reset()
	}

	rest := script
	for rest != "" {
		switch {
		case strings.HasPrefix(rest, "--"): // line comment: swallow to newline
			if end := strings.IndexByte(rest, '\n'); end >= 0 {
				rest = rest[end:] // keep the newline itself
			} else {
				rest = ""
			}
		case rest[0] == '\'': // single-quoted string: swallow whole literal
			sb.WriteByte(rest[0])
			rest = rest[1:]
			for rest != "" {
				if rest[0] == '\'' {
					if len(rest) > 1 && rest[1] == '\'' { // '' escape
						sb.WriteString("''")
						rest = rest[2:]
						continue
					}
					break
				}
				sb.WriteByte(rest[0])
				rest = rest[1:]
			}
			if rest != "" {
				sb.WriteByte('\'')
				rest = rest[1:]
			}
		case rest[0] == '$': // dollar-quoted body: $$..$$ or $tag$..$tag$
			if end := strings.IndexByte(rest[1:], '$'); end >= 0 {
				tag := rest[:end+2]
				if close := strings.Index(rest[len(tag):], tag); close >= 0 {
					body := rest[:len(tag)+close+len(tag)]
					sb.WriteString(body)
					rest = rest[len(body):]
					continue
				}
			}
			sb.WriteByte(rest[0]) // lone $, not a dollar quote
			rest = rest[1:]
		case rest[0] == ';': // statement terminator
			flush()
			rest = rest[1:]
		default:
			sb.WriteByte(rest[0])
			rest = rest[1:]
		}
	}
	flush()
	return out
}
