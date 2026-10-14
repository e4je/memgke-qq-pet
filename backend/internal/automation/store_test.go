package automation

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeBacksUpLegacyDataWithoutDeletingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qq-pet.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE qq_bindings(id INTEGER PRIMARY KEY,qq_number TEXT);INSERT INTO qq_bindings VALUES(1,'123');CREATE TABLE wallets(user_id INTEGER,coins INTEGER);INSERT INTO wallets VALUES(1,777);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = os.Stat(path + ".pre-v2.db"); err != nil {
		t.Fatal("upgrade backup missing")
	}
	var coins int
	if err = store.DB.QueryRow("SELECT coins FROM wallets WHERE user_id=1").Scan(&coins); err != nil || coins != 777 {
		t.Fatal("legacy data lost")
	}
	a, err := store.Account(context.Background(), 123)
	if err != nil || a.Config.Enabled || a.Revision != 1 {
		t.Fatalf("unexpected imported config: %+v %v", a, err)
	}
	a.Config.Enabled = true
	if err = store.Configure(context.Background(), 123, a.Config, a.Revision); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	a, err = store.Account(context.Background(), 123)
	if err != nil || !a.Config.Enabled {
		t.Fatal("startup overwrote reviewed config")
	}
}
