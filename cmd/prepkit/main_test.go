package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestCheckHealth(t *testing.T) {
	status := http.StatusOK
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login" {
			t.Errorf("probed %s, want /login", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	addr := ts.Listener.Addr().String()

	if err := checkHealth(addr); err != nil {
		t.Errorf("healthy server: %v", err)
	}
	status = http.StatusInternalServerError
	if err := checkHealth(addr); err == nil {
		t.Error("a 500 must report unhealthy")
	}
	ts.Close()
	if err := checkHealth(addr); err == nil {
		t.Error("a stopped server must report unhealthy")
	}
}

// The binary promises no outbound connections, so the probe must refuse
// anything but loopback before it dials.
func TestCheckHealthRefusesNonLoopback(t *testing.T) {
	err := checkHealth("192.0.2.1:8214")
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Errorf("got %v, want a non-loopback refusal", err)
	}
}
