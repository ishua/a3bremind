package store

import (
	"database/sql"
	"fmt"
)

// scannable is implemented by *sql.Row and *sql.Rows.
type scannable interface {
	Scan(dest ...interface{}) error
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
