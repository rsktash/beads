package config

import "testing"

func TestStripPassword(t *testing.T) {
	cases := []struct {
		in, want string
		had      bool
	}{
		{"postgres://user:pw@host/db", "postgres://user@host/db", true},
		{"postgres://user@host/db", "postgres://user@host/db", false},
		{"postgresql://u:p@h:5432/d?sslmode=disable", "postgresql://u@h:5432/d?sslmode=disable", true},
		{"root:secret@tcp(127.0.0.1:3306)/yuklar", "root@tcp(127.0.0.1:3306)/yuklar", true},
		{"root@tcp(127.0.0.1:3306)/yuklar", "root@tcp(127.0.0.1:3306)/yuklar", false},
		{"sqlite:/tmp/x.db", "sqlite:/tmp/x.db", false},
		{"/tmp/x.db", "/tmp/x.db", false},
	}
	for _, c := range cases {
		got, had := stripPassword(c.in)
		if got != c.want || had != c.had {
			t.Errorf("stripPassword(%q) = (%q, %v); want (%q, %v)", c.in, got, had, c.want, c.had)
		}
	}
}

func TestInjectPassword(t *testing.T) {
	cases := []struct {
		dsn, pw, want string
	}{
		{"postgres://u@h/d", "pw", "postgres://u:pw@h/d"},
		{"postgres://u:already@h/d", "pw", "postgres://u:already@h/d"},
		{"postgresql://u@h:5432/d?x=1", "pw", "postgresql://u:pw@h:5432/d?x=1"},
		{"root@tcp(h:3306)/db", "secret", "root:secret@tcp(h:3306)/db"},
		{"root:already@tcp(h:3306)/db", "secret", "root:already@tcp(h:3306)/db"},
		{"sqlite:/tmp/x.db", "pw", "sqlite:/tmp/x.db"},
		{"postgres://u@h/d", "", "postgres://u@h/d"},
	}
	for _, c := range cases {
		got := injectPassword(c.dsn, c.pw)
		if got != c.want {
			t.Errorf("injectPassword(%q, %q) = %q; want %q", c.dsn, c.pw, got, c.want)
		}
	}
}
