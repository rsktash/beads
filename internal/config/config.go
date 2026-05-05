// Package config resolves where the beads database lives and which prefix the
// project's IDs use.
//
// Resolution order:
//  1. --db / BEADS_DB explicit DSN
//  2. .beads/config (key=value) in cwd or ancestor
//  3. .beads/beads.db default in cwd
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
	dirName       = ".beads"
	configName    = "config"
	defaultName   = "beads.db"
	DefaultPrefix = "bd"
	EnvDSN        = "BEADS_DB"
	EnvDir        = "BEADS_DIR"
	EnvPrefix     = "BEADS_PREFIX"
)

type Config struct {
	DSN     string
	BeadDir string
	Prefix  string
}

func Resolve(explicitDSN string) (*Config, error) {
	cfg := &Config{Prefix: DefaultPrefix}
	if explicitDSN != "" {
		cfg.DSN = explicitDSN
	}
	if env := os.Getenv(EnvDSN); env != "" && cfg.DSN == "" {
		cfg.DSN = env
	}

	dir, found, err := findBeadsDir()
	if err != nil {
		return nil, err
	}
	if found {
		cfg.BeadDir = dir
		if err := readConfig(dir, cfg); err != nil {
			return nil, err
		}
	}

	if env := os.Getenv(EnvPrefix); env != "" {
		cfg.Prefix = env
	}
	if cfg.DSN == "" {
		if !found {
			return nil, errors.New("no .beads directory found — run `bd init` first")
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

// readConfig parses .beads/config and overrides cfg fields. Missing file is
// not an error.
func readConfig(beadsDir string, cfg *Config) error {
	p := filepath.Join(beadsDir, configName)
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
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
		switch strings.TrimSpace(k) {
		case "db":
			if cfg.DSN == "" {
				cfg.DSN = strings.TrimSpace(v)
			}
		case "prefix":
			cfg.Prefix = strings.TrimSpace(v)
		}
	}
	return sc.Err()
}

// PrefixFromDSN extracts a sensible default prefix from a DSN. For
// `postgres://user@host/dbname` or `mysql ... /dbname` it returns the
// database name; for a sqlite path it returns the filename without
// extension; falls back to DefaultPrefix.
func PrefixFromDSN(dsn string) string {
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		// postgres://user:pw@host:port/dbname?...
		rest := dsn
		if i := strings.Index(rest, "://"); i >= 0 {
			rest = rest[i+3:]
		}
		if slash := strings.Index(rest, "/"); slash >= 0 {
			rest = rest[slash+1:]
			if q := strings.Index(rest, "?"); q >= 0 {
				rest = rest[:q]
			}
			if rest != "" {
				return rest
			}
		}
	case strings.Contains(dsn, "tcp(") && strings.Contains(dsn, ")/"):
		// MySQL DSN: user[:pw]@tcp(host:port)/dbname?...
		i := strings.Index(dsn, ")/")
		if i >= 0 {
			rest := dsn[i+2:]
			if q := strings.Index(rest, "?"); q >= 0 {
				rest = rest[:q]
			}
			if rest != "" {
				return rest
			}
		}
	}
	// sqlite or unknown: filename minus extension
	base := dsn
	if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
		base = base[i+1:]
	}
	for _, ext := range []string{".db", ".sqlite", ".sqlite3"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}
	if base == "" || base == "beads" {
		return DefaultPrefix
	}
	return base
}

// Init creates a .beads dir at cwd with default config. dsn may be ""
// (default sqlite path), "sqlite:..." or "postgres://...". If prefix is
// empty, it's auto-derived from dsn via PrefixFromDSN.
func Init(dsn, prefix string) (*Config, error) {
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
	if prefix == "" {
		prefix = PrefixFromDSN(dsn)
	}
	cfgPath := filepath.Join(dir, configName)
	body := fmt.Sprintf("# beads config\ndb=%s\nprefix=%s\n", dsn, prefix)
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		return nil, err
	}
	return &Config{DSN: dsn, BeadDir: dir, Prefix: prefix}, nil
}
