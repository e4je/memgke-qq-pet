package main

import (
	"bufio"
	"path/filepath"
	"strings"
	"testing"

	"qq-pet/backend/internal/config"
	"qq-pet/backend/internal/database"
)

func TestStandaloneFirstAddressAndPersistedAddress(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ADDR", "127.0.0.1:2345")
	cfg := config.Config{}
	if err = loadOrInitializeAddress(db, &cfg, bufio.NewReader(strings.NewReader(""))); err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:2345" {
		t.Fatal(cfg.Addr)
	}
	t.Setenv("APP_ADDR", "127.0.0.1:9999")
	if err = loadOrInitializeAddress(db, &cfg, bufio.NewReader(strings.NewReader(""))); err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:2345" {
		t.Fatal("stored standalone address was overridden")
	}
}
