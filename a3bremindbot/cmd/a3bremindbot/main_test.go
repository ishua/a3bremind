package main

import (
	"testing"

	"github.com/a3bremind/a3bremindbot/internal/store"
	"github.com/stretchr/testify/require"
)

func TestMaxOpenConnsSet(t *testing.T) {
	// Verify that InitDB + SetMaxOpenConns(1) works without panicking.
	db, err := store.InitDB("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close() //nolint:errcheck // test cleanup

	db.SetMaxOpenConns(1)

	// A simple query to ensure the connection is usable.
	var result int
	err = db.QueryRow("SELECT 1").Scan(&result)
	require.NoError(t, err)
	require.Equal(t, 1, result)
}
