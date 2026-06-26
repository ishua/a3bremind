package store

import (
	"database/sql"
	"fmt"
)

// scannable is implemented by *sql.Row and *sql.Rows.
type scannable interface {
	Scan(dest ...interface{}) error
}

// Querier is implemented by *sql.DB and *sql.Tx.
// It allows store functions to be used in transactions.
type Querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// checkRowsAffected verifies that exactly one row was affected.
func checkRowsAffected(res sql.Result, entity, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%s %q not found", entity, id)
	}
	return nil
}
