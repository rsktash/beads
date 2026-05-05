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
	defaultName = "bd.db"
	EnvDSN      = "BD_DB"
	EnvDir      = "BD_DIR"
	// EnvPrefix is read by `bd init --prefix` only — outside init, the
	// prefix is read from the DB's `config` table.
	EnvPrefix = "BD_PREFIX"
)

type Config struct {
	DSN     string // resolved DB connection string
	BeadDir string // .bd directory path (may be empty if unresolved)
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
	return cfg, nil
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

// Init creates a .bd directory and writes the DSN to .bd/config.
// It does NOT seed the in-DB config table — that's done by the store layer
// once it has an open connection (because we need to be able to point
// `bd init` at remote DBs that may already have a config table populated).
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
	cfgPath := filepath.Join(dir, configName)
	body := fmt.Sprintf("# bd config — only the DSN lives here.\n# Project settings (prefix, id mode, custom statuses) live in the DB `config` table.\ndb=%s\n", dsn)
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		return nil, err
	}
	return &Config{DSN: dsn, BeadDir: dir}, nil
}
