package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"modernc.org/sqlite"
	"os"
	"path/filepath"
)

type Store struct{ DB *sql.DB }

var ErrRevision = errors.New("配置已被其他页面修改，请重新加载")

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*Store, error) { db.Close(); return nil, err }
	if _, err = db.Exec("PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"); err != nil {
		return fail(err)
	}
	var legacy int
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='qq_bindings'").Scan(&legacy); err != nil {
		return fail(err)
	}
	if legacy > 0 {
		backup := path + ".pre-v2.db"
		if _, e := os.Stat(backup); os.IsNotExist(e) {
			if _, err = db.Exec("VACUUM INTO ?", backup); err != nil {
				return fail(err)
			}
		}
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS pet_v2_accounts(self_id INTEGER PRIMARY KEY,config TEXT NOT NULL,state TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1);
 CREATE TABLE IF NOT EXISTS pet_v2_logs(id INTEGER PRIMARY KEY AUTOINCREMENT,self_id INTEGER NOT NULL,action TEXT NOT NULL,status TEXT NOT NULL,message TEXT NOT NULL,data TEXT,at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS pet_v2_logs_account ON pet_v2_logs(self_id,id);`)
	if err != nil {
		return fail(err)
	}
	cols, err := db.Query("PRAGMA table_info(pet_v2_logs)")
	if err != nil {
		return fail(err)
	}
	hasMeta := false
	for cols.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if err = cols.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
			cols.Close()
			return fail(err)
		}
		if name == "metadata" {
			hasMeta = true
		}
	}
	err = cols.Err()
	cols.Close()
	if err != nil {
		return fail(err)
	}
	if !hasMeta {
		if _, err = db.Exec("ALTER TABLE pet_v2_logs ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'"); err != nil {
			return fail(err)
		}
	}
	s := &Store{db}
	if legacy > 0 {
		if err = s.importLegacy(); err != nil {
			return fail(err)
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) Account(ctx context.Context, id int64) (Account, error) {
	a := Account{SelfID: id, Config: DefaultConfig(), State: State{Counts: map[string]int{}}}
	var c, v string
	err := s.DB.QueryRowContext(ctx, "SELECT config,state,revision FROM pet_v2_accounts WHERE self_id=?", id).Scan(&c, &v, &a.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	if err = json.Unmarshal([]byte(c), &a.Config); err != nil {
		return a, err
	}
	err = json.Unmarshal([]byte(v), &a.State)
	if a.State.Counts == nil {
		a.State.Counts = map[string]int{}
	}
	return a, err
}
func (s *Store) Configure(ctx context.Context, id int64, c Config, revision int) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(c)
	state, _ := json.Marshal(State{Counts: map[string]int{}})
	if revision == 0 {
		_, err := s.DB.ExecContext(ctx, "INSERT INTO pet_v2_accounts(self_id,config,state) VALUES(?,?,?)", id, string(b), string(state))
		if err != nil {
			var constraint *sqlite.Error
			if errors.As(err, &constraint) && constraint.Code() == 1555 {
				return ErrRevision
			}
			return err
		}
		return nil
	}
	result, err := s.DB.ExecContext(ctx, "UPDATE pet_v2_accounts SET config=?,revision=revision+1 WHERE self_id=? AND revision=?", string(b), id, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrRevision
	}
	return err
}
func (s *Store) Save(ctx context.Context, id int64, state State, entry *Log) error {
	state.UpdatedAt = now().Format("2006-01-02T15:04:05Z07:00")
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	defaults, _ := json.Marshal(DefaultConfig())
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO pet_v2_accounts(self_id,config,state) VALUES(?,?,?) ON CONFLICT(self_id) DO UPDATE SET state=excluded.state", id, string(defaults), string(b))
	if err != nil {
		return err
	}
	if entry != nil {
		data := string(safeData(entry.Data))
		if data == "" {
			data = "null"
		}
		meta, _ := json.Marshal(entry.Meta)
		_, err = tx.ExecContext(ctx, "INSERT INTO pet_v2_logs(self_id,action,status,message,data,at,metadata) VALUES(?,?,?,?,?,?,?)", id, entry.Action, entry.Status, entry.Message, data, state.UpdatedAt, string(meta))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM pet_v2_logs WHERE self_id=? AND (at<? OR id < COALESCE((SELECT id FROM pet_v2_logs WHERE self_id=? ORDER BY id DESC LIMIT 1 OFFSET 49999),0))", id, now().AddDate(0, 0, -7).Format("2006-01-02T15:04:05Z07:00"), id)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) Enabled(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT self_id FROM pet_v2_accounts WHERE json_extract(config,'$.enabled')=1 ORDER BY self_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (s *Store) Logs(ctx context.Context, id int64) ([]Log, error) {
	return s.QueryLogs(ctx, id, LogQuery{Limit: 200})
}

type LogQuery struct {
	Before                int64
	Limit                 int
	Status, Action, RunID string
}

func (s *Store) QueryLogs(ctx context.Context, id int64, q LogQuery) ([]Log, error) {
	if q.Limit <= 0 {
		q.Limit = 200
	}
	if q.Limit > 5000 {
		q.Limit = 5000
	}
	sql := "SELECT id,self_id,action,status,message,data,at,metadata FROM pet_v2_logs WHERE self_id=?"
	args := []any{id}
	if q.Before > 0 {
		sql += " AND id<?"
		args = append(args, q.Before)
	}
	if q.Status != "" {
		sql += " AND status=?"
		args = append(args, q.Status)
	}
	if q.Action != "" {
		sql += " AND action=?"
		args = append(args, q.Action)
	}
	if q.RunID != "" {
		sql += " AND json_extract(metadata,'$.run_id')=?"
		args = append(args, q.RunID)
	}
	sql += " ORDER BY id DESC LIMIT ?"
	args = append(args, q.Limit)
	rows, err := s.DB.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Log{}
	for rows.Next() {
		var l Log
		var data, meta string
		if err = rows.Scan(&l.ID, &l.SelfID, &l.Action, &l.Status, &l.Message, &data, &l.At, &meta); err != nil {
			return nil, err
		}
		l.Data = json.RawMessage(data)
		_ = json.Unmarshal([]byte(meta), &l.Meta)
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *Store) AppendLog(ctx context.Context, id int64, l Log) error {
	if id <= 0 {
		return nil
	}
	l.Data = safeData(l.Data)
	data := string(l.Data)
	if data == "" {
		data = "null"
	}
	meta, _ := json.Marshal(l.Meta)
	_, err := s.DB.ExecContext(ctx, "INSERT INTO pet_v2_logs(self_id,action,status,message,data,at,metadata) VALUES(?,?,?,?,?,?,?)", id, l.Action, l.Status, l.Message, data, now().Format("2006-01-02T15:04:05.000Z07:00"), string(meta))
	if err == nil && l.Action == "tick" && l.Status == "complete" {
		_, err = s.DB.ExecContext(ctx, "DELETE FROM pet_v2_logs WHERE self_id=? AND (at<? OR id < COALESCE((SELECT id FROM pet_v2_logs WHERE self_id=? ORDER BY id DESC LIMIT 1 OFFSET 49999),0))", id, now().AddDate(0, 0, -7).Format("2006-01-02T15:04:05Z07:00"), id)
	}
	return err
}

// Legacy rows remain untouched. Import only settings backed by current APIs,
// disabled for one review because old task completion semantics were different.
func (s *Store) importLegacy() error {
	rows, err := s.DB.Query("SELECT qq_number FROM qq_bindings")
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		c := DefaultConfig()
		state := State{Counts: map[string]int{}, Message: "旧数据已备份。请核对新版计划后启用自动化。"}
		var exists int
		_ = s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='pet_auto_control_configs'").Scan(&exists)
		if exists > 0 {
			var feed, bathe bool
			var hunger, clean int
			if s.DB.QueryRow(`SELECT c.auto_feed_enabled,c.auto_feed_threshold,c.auto_bathe_enabled,c.auto_bathe_threshold FROM pet_auto_control_configs c JOIN qq_bindings q ON q.id=c.qq_binding_id WHERE q.qq_number=?`, id).Scan(&feed, &hunger, &bathe, &clean) == nil {
				c.Care.Feed = feed
				c.Care.Hunger = hunger
				c.Care.Cleanliness = clean
				state.Message += " 旧用品名称和计划请从官方目录重新选择。"
			}
		}
		config, _ := json.Marshal(c)
		v, _ := json.Marshal(state)
		if _, err = s.DB.Exec("INSERT OR IGNORE INTO pet_v2_accounts(self_id,config,state) VALUES(?,?,?)", id, string(config), string(v)); err != nil {
			return err
		}
	}
	return nil
}
