package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Backup and restore for Section 4.6: VACUUM INTO copies before every
// update and once a day, keeping 7 daily and 4 weekly copies.
// Restoring is replacing files with the daemon stopped.

// BackupNames lists backup files for dbPath in dir, newest first.
func BackupNames(dir, prefix string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".db") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

// BackupsDirFor returns the backups directory for a database path:
// <dbdir>/backups, or <root>/backups when the layout matches.
func BackupsDirFor(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), "backups")
}

// Backup copies dbPath into dir with a timestamped name and rotates
// to 7 daily + 4 weekly copies (Section 4.6). It runs VACUUM INTO on
// a short-lived read connection so the porter never blocks.
func Backup(ctx context.Context, dbPath, dir, prefix string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	name := fmt.Sprintf("%s%s.db", prefix, stamp)
	dst := filepath.Join(dir, name)
	quoted := "'" + strings.ReplaceAll(dst, "'", "''") + "'"
	if err := vacuumInto(ctx, dbPath, quoted); err != nil {
		return "", err
	}
	if err := rotateBackups(dir, prefix); err != nil {
		return "", err
	}
	return dst, nil
}

// openRaw opens a short-lived handle for maintenance (backup,
// restore verification). It never joins the porter.
func openRaw(dbPath string) (*sql.DB, error) {
	setupDriver()
	return sql.Open(sqliteDriverName, dbPath)
}

// vacuumInto runs VACUUM INTO on its own connection.
func vacuumInto(ctx context.Context, dbPath, quotedDst string) error {
	setupDriver()
	db, err := openRaw(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.ExecContext(ctx, "VACUUM INTO "+quotedDst)
	return err
}

// rotateBackups keeps 7 daily and 4 weekly copies: per calendar day
// the newest copy is the daily; per ISO week the newest daily is the
// weekly. Everything else is deleted (Section 4.6).
func rotateBackups(dir, prefix string) error {
	names, err := BackupNames(dir, prefix)
	if err != nil {
		return err
	}
	byDay := map[string]string{}
	var days []string
	for _, full := range names {
		base := filepath.Base(full)
		rest := strings.TrimSuffix(strings.TrimPrefix(base, prefix), ".db")
		if len(rest) < 8 {
			continue
		}
		day := rest[:8]
		if _, ok := byDay[day]; !ok {
			byDay[day] = full
			days = append(days, day)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	keep := map[string]bool{}
	for i, d := range days {
		if i < 7 {
			keep[byDay[d]] = true
		}
	}
	// Weekly: newest kept daily per ISO week, up to 4 beyond dailies.
	weeks := map[string]string{}
	for _, d := range days {
		t, err := time.Parse("20060102", d)
		if err != nil {
			continue
		}
		y, w := t.ISOWeek()
		key := fmt.Sprintf("%04d-W%02d", y, w)
		if _, ok := weeks[key]; !ok {
			weeks[key] = byDay[d]
		}
	}
	var weekDays []string
	for _, full := range weeks {
		weekDays = append(weekDays, full)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(weekDays)))
	for i, full := range weekDays {
		if i < 4 {
			keep[full] = true
		}
	}
	for _, full := range names {
		if !keep[full] {
			_ = os.Remove(full)
		}
	}
	return nil
}

// restoreLatest copies the newest backup over dbPath and returns true.
// The caller must reopen afterwards: handles may cache the corrupt
// image (Section 4.3).
func restoreLatest(ctx context.Context, dbPath, dir string) (bool, error) {
	prefix := backupPrefixFor(dbPath)
	names, err := BackupNames(dir, prefix)
	if err != nil {
		return false, err
	}
	_ = ctx
	if len(names) == 0 {
		return false, nil
	}
	src := names[0]
	data, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}
	tmp := dbPath + ".restore-tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	// Drop stale WAL/SHM: the restored copy starts clean.
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	return true, nil
}

// Restore replaces dbPath with the newest backup in dir. The daemon
// must be stopped (Section 4.6).
func Restore(dbPath, dir string) error {
	ok, err := restoreLatest(context.Background(), dbPath, dir)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("store: no backups for %s", dbPath)
	}
	return nil
}

// backupPrefixFor derives the backup prefix from the db filename:
// excalibur.db -> excalibur-.
func backupPrefixFor(dbPath string) string {
	base := filepath.Base(dbPath)
	return strings.TrimSuffix(base, ".db") + "-"
}
