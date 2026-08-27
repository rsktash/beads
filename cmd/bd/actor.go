package main

import (
	"os"
)

// resolveActor returns the filed_by identity and whether the caller is an executor.
// BD_ACTOR unset means owner (not executor). "coordinator" is not executor.
// Every other value, including the empty string set explicitly, is executor.
// The identity combines the actor word with the OS user from assigneeFromEnv.
func resolveActor() (string, bool) {
	v, ok := os.LookupEnv("BD_ACTOR")
	user := assigneeFromEnv()
	if !ok {
		return "owner:" + user, false
	}
	if v == "coordinator" {
		return "coordinator:" + user, false
	}
	// Every other value, including "" explicitly set, is executor.
	// Preserve the raw value as actor word (so "banana" stays "banana").
	return v + ":" + user, true
}
