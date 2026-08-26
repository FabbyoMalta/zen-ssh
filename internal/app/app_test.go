package app

import (
	"slices"
	"testing"

	"zenssh/internal/config"
)

func TestEnvironmentWithTermOverridesExistingValue(t *testing.T) {
	got := environmentWithTerm([]string{"PATH=/bin", "TERM=xterm-256color"}, config.TermXterm)
	want := []string{"PATH=/bin", "TERM=xterm"}
	if !slices.Equal(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestEnvironmentWithTermPreservesSystemDefault(t *testing.T) {
	env := []string{"TERM=xterm-256color"}
	got := environmentWithTerm(env, config.TermSystem)
	if !slices.Equal(got, env) {
		t.Fatalf("environment = %#v, want %#v", got, env)
	}
}
