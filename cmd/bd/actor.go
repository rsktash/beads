package main

import (
	"fmt"
	"os"
	"strings"
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

// executorRefusal is the single source of the executor refusal message. The
// execution skills quote this string, so every actor-gated write path must
// return it byte-for-byte rather than composing its own wording — hence one
// fixed wording ("file rulings") even for callers gating a different verb
// (e.g. `bd question close`): the point is a caller can recognize the
// refusal from this one string alone, not that the verb matches the command.
func executorRefusal(identity string) error {
	word := actorWord(identity)
	return fmt.Errorf("BD_ACTOR=%s: executors cannot file rulings; a coordinator or the owner (unset BD_ACTOR) may — executors may file findings or questions instead", word)
}

// actorWord is the bare actor token (before the ":user" suffix resolveActor
// appends) that error messages report back to the caller.
func actorWord(identity string) string {
	word := identity
	if idx := strings.IndexByte(word, ':'); idx >= 0 {
		word = word[:idx]
	}
	if word == "" {
		word = "executor"
	}
	return word
}
