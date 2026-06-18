package scanner

import (
	"testing"
)

func TestUserAgentPool_Random(t *testing.T) {
	p := NewUserAgentPool()
	if len(p.agents) < 5 {
		t.Errorf("pool should have multiple agents, got %d", len(p.agents))
	}
	// Verify randomness returns different values (not guaranteed but very likely)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		ua := p.Random()
		if ua == "" {
			t.Error("empty UA")
		}
		seen[ua] = true
	}
	if len(seen) < 3 {
		t.Errorf("expected at least 3 unique UAs in 20 random picks, got %d", len(seen))
	}
}

func TestUserAgentPool_ContainsBrowsers(t *testing.T) {
	p := NewUserAgentPool()
	hasChrome, hasFirefox, hasSafari := false, false, false
	for _, ua := range p.agents {
		switch {
		case contains(ua, "Chrome"):
			hasChrome = true
		case contains(ua, "Firefox"):
			hasFirefox = true
		case contains(ua, "Safari"):
			hasSafari = true
		}
	}
	if !hasChrome {
		t.Error("pool should contain Chrome")
	}
	if !hasFirefox {
		t.Error("pool should contain Firefox")
	}
	if !hasSafari {
		t.Error("pool should contain Safari")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}