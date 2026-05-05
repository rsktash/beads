// Package config resolves where the beads database lives. It is intentionally
// minimal: the on-disk file holds only what's needed before the DB is open
// (the DSN). Everything else (prefix, id mode, custom statuses/types) lives
// in the DB's `config` table so all clients of the same DB see the same
// project settings.
//
// Resolution order for the DSN:
//  1. --db / $BD_DB explicit DSN
//  2. db= line in .bd/config (walked up from cwd, or $BD_DIR override)
//  3. default: .bd/bd.db (sqlite)
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	dirName     = ".bd"
	configName  = "config"
	envFileName = ".env"
	defaultName = "bd.db"
	EnvDSN      = "BD_DB"
	EnvDir      = "BD_DIR"
	// EnvPrefix is read by `bd init --prefix` only — outside init, the
	// prefix is read from the DB's `config` table.
	EnvPrefix = "BD_PREFIX"
	// EnvDBPassword supplies the password for the active store DSN at
	// connect time. The DSN saved on disk in .bd/config is sanitised to
	// not contain credentials; this is the runtime injection point.
	EnvDBPassword = "BD_DB_PASSWORD"
)

type Config struct {
	DSN        string // resolved DB connection string (with password if applicable)
	BeadDir    string // .bd directory path (may be empty if unresolved)
	DisplayDSN string // sanitised DSN safe for logs/stdout (no password)
}

// Resolve returns the connection config for the active project. It does NOT
// read any project-level settings (prefix, id mode, etc.); those live in the
// DB and are resolved by the store layer after Open().
func Resolve(explicitDSN string) (*Config, error) {
	cfg := &Config{}
	if explicitDSN != "" {
		cfg.DSN = explicitDSN
	} else if env := os.Getenv(EnvDSN); env != "" {
		cfg.DSN = env
	}

	dir, found, err := findBeadsDir()
	if err != nil {
		return nil, err
	}
	if found {
		cfg.BeadDir = dir
		if cfg.DSN == "" {
			dsn, err := readDSNFromFile(dir)
			if err != nil {
				return nil, err
			}
			cfg.DSN = dsn
		}
	}
	if cfg.DSN == "" {
		if !found {
			return nil, errors.New("no .bd directory found — run `bd init` first")
		}
		cfg.DSN = filepath.Join(dir, defaultName)
	}
	cfg.DisplayDSN, _ = stripPassword(cfg.DSN)
	cfg.DSN = injectPassword(cfg.DSN, lookupDBPassword(cfg.BeadDir))
	return cfg, nil
}

// lookupDBPassword resolves the store DSN password from $BD_DB_PASSWORD or a
// .env file (cwd, then beadsDir). Returns "" if nothing is set.
func lookupDBPassword(beadsDir string) string {
	if v := os.Getenv(EnvDBPassword); v != "" {
		return v
	}
	candidates := []string{envFileName}
	if beadsDir != "" {
		candidates = append(candidates, filepath.Join(beadsDir, envFileName))
	}
	for _, p := range candidates {
		if v := readEnvKey(p, EnvDBPassword); v != "" {
			return v
		}
	}
	return ""
}

// ReadEnvKey is exported for cmd/bd/migrate.go to share the dotenv parser.
func ReadEnvKey(path, key string) string { return readEnvKey(path, key) }

func readEnvKey(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	prefix := key + "="
	exportPrefix := "export " + prefix
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, exportPrefix) {
			line = strings.TrimPrefix(line, "export ")
		}
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		return strings.Trim(v, `"'`)
	}
	return ""
}

// injectPassword adds password into a DSN if the DSN doesn't already include
// credentials. Supports postgres://user@host/db, postgresql://, and the
// MySQL go-sql-driver form user@tcp(host:port)/db.
func injectPassword(dsn, pw string) string {
	if pw == "" {
		return dsn
	}
	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		return injectURIPassword(dsn, pw)
	case strings.Contains(dsn, "@tcp("):
		return injectMySQLPassword(dsn, pw)
	}
	return dsn // sqlite paths or unknown schemes are returned unchanged
}

func injectURIPassword(dsn, pw string) string {
	// postgres://[user[:pw]@]host/db?...
	scheme, rest, ok := strings.Cut(dsn, "://")
	if !ok {
		return dsn
	}
	atIdx := strings.Index(rest, "@")
	if atIdx <= 0 {
		// no userinfo at all — nothing to do, password without user is meaningless
		return dsn
	}
	cred, hostPath := rest[:atIdx], rest[atIdx:]
	if strings.Contains(cred, ":") {
		return dsn // already has password
	}
	return scheme + "://" + cred + ":" + pw + hostPath
}

func injectMySQLPassword(dsn, pw string) string {
	at := strings.Index(dsn, "@")
	if at <= 0 {
		return dsn
	}
	cred := dsn[:at]
	if strings.Contains(cred, ":") {
		return dsn
	}
	return cred + ":" + pw + dsn[at:]
}

// stripPassword returns the DSN with any embedded password removed. Used by
// Init to avoid persisting credentials in .bd/config.
func stripPassword(dsn string) (sanitised string, hadPassword bool) {
	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		scheme, rest, ok := strings.Cut(dsn, "://")
		if !ok {
			return dsn, false
		}
		atIdx := strings.Index(rest, "@")
		if atIdx <= 0 {
			return dsn, false
		}
		cred, hostPath := rest[:atIdx], rest[atIdx:]
		colon := strings.Index(cred, ":")
		if colon < 0 {
			return dsn, false
		}
		return scheme + "://" + cred[:colon] + hostPath, true
	case strings.Contains(dsn, "@tcp("):
		at := strings.Index(dsn, "@")
		cred := dsn[:at]
		colon := strings.Index(cred, ":")
		if colon < 0 {
			return dsn, false
		}
		return cred[:colon] + dsn[at:], true
	}
	return dsn, false
}

func findBeadsDir() (string, bool, error) {
	if env := os.Getenv(EnvDir); env != "" {
		return env, true, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	cur := cwd
	for {
		candidate := filepath.Join(cur, dirName)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, true, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return "", false, nil
}

func readDSNFromFile(beadsDir string) (string, error) {
	p := filepath.Join(beadsDir, configName)
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == "db" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", sc.Err()
}

// Init creates a .bd directory and writes the (sanitised) DSN to .bd/config.
// If the supplied DSN contains a password, it is stripped before writing and
// the caller is informed so they can set BD_DB_PASSWORD instead.
//
// Init does NOT seed the in-DB config table — that is the caller's job, after
// opening the store.
func Init(dsn string) (*Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cwd, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if dsn == "" {
		dsn = filepath.Join(dir, defaultName)
	}
	sanitised, hadPW := stripPassword(dsn)

	cfgPath := filepath.Join(dir, configName)
	body := "# bd config — only the DSN lives here.\n" +
		"# Project settings (prefix, id mode, custom statuses) live in the DB `config` table.\n" +
		"# Credentials must NOT be embedded here. Set BD_DB_PASSWORD in the env or in .bd/.env.\n" +
		fmt.Sprintf("db=%s\n", sanitised)
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		return nil, err
	}
	// For the immediate Open call after Init, fold in the password from
	// $BD_DB_PASSWORD or .env in cwd / .bd/.env. injectPassword is a no-op
	// when the DSN already includes credentials.
	runtime := injectPassword(dsn, lookupDBPassword(dir))
	cfg := &Config{
		DSN:        runtime,
		BeadDir:    dir,
		DisplayDSN: sanitised,
	}
	if hadPW {
		fmt.Fprintln(os.Stderr,
			"note: password stripped from DSN before saving to .bd/config — "+
				"export BD_DB_PASSWORD or add it to .bd/.env so future runs can connect.")
	}
	return cfg, nil
}
