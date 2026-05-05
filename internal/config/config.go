// Package config resolves where the beads database lives.
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
	dirName     = ".beads"
	configName  = "config"
	defaultName = "beads.db"
	EnvDSN      = "BEADS_DB"
	EnvDir      = "BEADS_DIR"
)

type Config struct {
	DSN     string
	BeadDir string
}

func Resolve(explicit string) (*Config, error) {
	if explicit != "" {
		return &Config{DSN: explicit}, nil
	}
	if env := os.Getenv(EnvDSN); env != "" {
		return &Config{DSN: env}, nil
	}
	dir, err := findBeadsDir()
	if err != nil {
		return nil, err
	}
	if dsn, ok, err := readConfigDSN(dir); err != nil {
		return nil, err
	} else if ok {
		return &Config{DSN: dsn, BeadDir: dir}, nil
	}
	return &Config{DSN: filepath.Join(dir, defaultName), BeadDir: dir}, nil
}

func findBeadsDir() (string, error) {
	if env := os.Getenv(EnvDir); env != "" {
		return env, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	cur := cwd
	for {
		candidate := filepath.Join(cur, dirName)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return "", errors.New("no .beads directory found — run `bd init` first")
}

func readConfigDSN(beadsDir string) (string, bool, error) {
	p := filepath.Join(beadsDir, configName)
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
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
			return strings.TrimSpace(v), true, nil
		}
	}
	return "", false, sc.Err()
}

// Init creates a .beads dir at cwd with default config pointing to a sqlite db.
// dsn may be "" (default sqlite), "sqlite:..." or "postgres://...".
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
	body := fmt.Sprintf("# beads config\ndb=%s\n", dsn)
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		return nil, err
	}
	return &Config{DSN: dsn, BeadDir: dir}, nil
}
