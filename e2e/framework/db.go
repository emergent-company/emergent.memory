// Package framework — db.go
//
// MemoryDB provides a direct Postgres connection to the test server's database
// for tests that require DB-level operations (email jobs, superadmin roles,
// template packs) with no equivalent API endpoint.
//
// Usage:
//
//		db, err := framework.MemoryDB()
//		if err != nil {
//		    t.Skipf("memory DB not available: %v", err)
//		}
//	     // Do NOT close db — it is a shared singleton.
//
// Schema-qualified queries: email.*, core.*, kb.*
package e2eframework

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
)

var (
	memDB     *sql.DB
	memDBOnce sync.Once
	memDBErr  error
)

// MemoryDB returns a *sql.DB connected to the test server's Postgres database.
// The connection is established once and reused across calls.  Connection
// parameters are read from POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER,
// POSTGRES_PASSWORD, and POSTGRES_DB environment variables with defaults
// matching the localhost test configuration.
func MemoryDB() (*sql.DB, error) {
	memDBOnce.Do(func() {
		host := envDefault("POSTGRES_HOST", "127.0.0.1")
		port := envDefault("POSTGRES_PORT", "5436")
		user := envDefault("POSTGRES_USER", "emergent")
		pass := envDefault("POSTGRES_PASSWORD", "emergent")
		dbname := envDefault("POSTGRES_DB", "emergent")

		dsn := fmt.Sprintf(
			"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, pass, dbname,
		)

		memDB, memDBErr = sql.Open("postgres", dsn)
		if memDBErr != nil {
			return
		}
		memDBErr = memDB.Ping()
		if memDBErr != nil {
			memDB.Close()
			memDB = nil
		}
	})
	return memDB, memDBErr
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
