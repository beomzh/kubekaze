package redact

import (
	"strings"
	"testing"
)

func TestLineMasksSecrets(t *testing.T) {
	secrets := map[string]string{
		"DB_PASSWORD=hunter2 started":                          "hunter2",
		`{"api_key": "abc123def456"}`:                          "abc123def456",
		"token: s3cr3tvalue":                                   "s3cr3tvalue",
		"Authorization: Bearer abcdef0123456789abcdef":         "abcdef0123456789abcdef",
		"using AKIAIOSFODNN7EXAMPLE for s3":                    "AKIAIOSFODNN7EXAMPLE",
		"postgres://admin:p4ssw0rd@db:5432/app":                "p4ssw0rd",
		"jwt eyJhbGciOiJIUzI1.eyJzdWIiOiIxMjM0NTY.SflKxwRJSMeK": "eyJhbGciOiJIUzI1",
		"ghp_abcdefghijklmnopqrstuvwxyz0123456789":             "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
	}
	for in, secret := range secrets {
		got := Line(in)
		if strings.Contains(got, secret) {
			t.Errorf("secret %q leaked: %q -> %q", secret, in, got)
		}
		if !strings.Contains(got, "[REDACTED]") {
			t.Errorf("no mask in %q", got)
		}
	}
}

func TestLineKeepsOrdinaryText(t *testing.T) {
	for _, s := range []string{
		"Back-off restarting failed container",
		"listening on :8080",
		"user logged in from 10.0.0.1",
		"password policy loaded", // word without assignment
	} {
		if got := Line(s); got != s {
			t.Errorf("Line(%q) changed to %q", s, got)
		}
	}
}
