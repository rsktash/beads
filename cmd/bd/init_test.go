package main

import "testing"

func TestWithSearchPath(t *testing.T) {
	cases := []struct {
		dsn, prefix, want string
	}{
		// postgres URI: search_path appended
		{
			"postgres://user@host/tracker?sslmode=disable",
			"yuklar",
			"postgres://user@host/tracker?search_path=yuklar&sslmode=disable",
		},
		{
			"postgresql://u@h:5432/t",
			"api",
			"postgresql://u@h:5432/t?search_path=api",
		},
		// already specified: respect user's choice
		{
			"postgres://user@host/tracker?search_path=other",
			"yuklar",
			"postgres://user@host/tracker?search_path=other",
		},
		// sqlite: untouched
		{".bd/bd.db", "yuklar", ".bd/bd.db"},
		{"sqlite:/tmp/x.db", "y", "sqlite:/tmp/x.db"},
		// mysql DSN: untouched (search_path is postgres-only)
		{"root@tcp(localhost:3306)/db", "y", "root@tcp(localhost:3306)/db"},
		// empty: untouched
		{"", "y", ""},
	}
	for _, c := range cases {
		got := withSearchPath(c.dsn, c.prefix)
		if got != c.want {
			t.Errorf("withSearchPath(%q, %q) = %q; want %q", c.dsn, c.prefix, got, c.want)
		}
	}
}
