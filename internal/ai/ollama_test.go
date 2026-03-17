package ai

import (
	"testing"
)

func TestNormaliseAction(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"rollback", "rollback"},
		{"ROLLBACK", "rollback"},
		{"revert", "rollback"},
		{"undo", "rollback"},
		{"restart", "restart"},
		{"ack", "ack"},
		{"acknowledged", "ack"},
		{"resolve", "resolve"},
		{"resolved", "resolve"},
		{"scale:3", "scale:3"},
		{"scale:10", "scale:10"},
		{"random text", "ignore"},
		{"", "ignore"},
		{`"rollback"`, "rollback"},
	}
	for _, c := range cases {
		got := normaliseAction(c.raw)
		if got != c.want {
			t.Errorf("normaliseAction(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestBuildSuggestPrompt(t *testing.T) {
	p := buildSuggestPrompt("High memory usage", "critical", "production")
	for _, want := range []string{"High memory usage", "critical", "production", "Kubernetes"} {
		if !containsStr(p, want) {
			t.Errorf("suggest prompt missing %q", want)
		}
	}
}

func TestBuildParsePrompt(t *testing.T) {
	p := buildParsePrompt("rollback la api", "API latency > 5s")
	for _, want := range []string{"rollback la api", "API latency > 5s", "rollback"} {
		if !containsStr(p, want) {
			t.Errorf("parse prompt missing %q", want)
		}
	}
}

func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
