package main

import "testing"

func TestVersionCommand(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"-version"}, {"--version"}} {
		if code := run(args); code != 0 {
			t.Errorf("run(%v) = %d, want 0", args, code)
		}
	}
}

func TestBuildVersionPrefersStamp(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "v9.9.9"
	if got := buildVersion(); got != "v9.9.9" {
		t.Errorf("buildVersion() = %q", got)
	}
}
