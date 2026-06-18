package fingerprint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidFile(t *testing.T) {
	tmp := writeTestFingerprint(t)

	db, err := Load(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if db == nil {
		t.Fatal("db is nil")
	}
	if len(db.Technologies) != 1 {
		t.Errorf("want 1 tech, got %d", len(db.Technologies))
	}
}

func TestGetByCategory(t *testing.T) {
	tmp := writeTestFingerprint(t)

	_, _ = Load(tmp)
	tests := GetByCategory("cms")
	if len(tests) != 1 {
		t.Errorf("want 1 in 'cms', got %d", len(tests))
	}
	if tests[0].Slug != "wordpress" {
		t.Errorf("want 'wordpress', got %q", tests[0].Slug)
	}
}

func TestFindBySlug(t *testing.T) {
	tmp := writeTestFingerprint(t)

	_, _ = Load(tmp)
	t1 := FindBySlug("wordpress")
	if t1 == nil {
		t.Fatal("FindBySlug returned nil")
	}
	if t1.Name != "WordPress" {
		t.Errorf("want name 'WordPress', got %q", t1.Name)
	}
}

func writeTestFingerprint(t *testing.T) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "test.json")
	content := `{
  "version": "1.0.0",
  "updated": "2026-06-17",
  "technologies": [
    {
      "slug": "wordpress",
      "name": "WordPress",
      "category": "cms",
      "detectors": {
        "html": [{"pattern": "/wp-content/"}],
        "meta": [{"name": "generator", "pattern": "WordPress"}]
      },
      "implies": ["PHP", "MySQL"]
    }
  ]
}`
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return tmp
}