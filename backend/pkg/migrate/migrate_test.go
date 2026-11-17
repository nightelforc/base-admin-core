package migrate

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/nightelforc/base-admin-core/backend/migrations"
)

func TestRenderReplacesPlaceholder(t *testing.T) {
	t.Parallel()

	got := Render("CREATE TABLE {{prefix}}users (id bigint); -- {{prefix}}", "base_")
	want := "CREATE TABLE base_users (id bigint); -- base_"
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestLoadSortsAndChecksums(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"0002_second.sql": {Data: []byte("SELECT 2;")},
		"0001_first.sql":  {Data: []byte("SELECT 1;")},
		"notes.txt":       {Data: []byte("ignored")},
	}

	files, err := Load(fsys, "base_")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("Load() returned %d files, want 2", len(files))
	}
	if files[0].Name != "0001_first.sql" || files[1].Name != "0002_second.sql" {
		t.Fatalf("Load() order = %s, %s", files[0].Name, files[1].Name)
	}
	if len(files[0].Checksum) != 64 {
		t.Fatalf("checksum length = %d, want 64 hex chars", len(files[0].Checksum))
	}
	if files[0].SQL != "SELECT 1;" {
		t.Fatalf("Load() did not render SQL: %q", files[0].SQL)
	}
}

func TestLoadRendersPrefix(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"0001_init.sql": {Data: []byte("CREATE TABLE {{prefix}}users (id bigint);")},
	}
	files, err := Load(fsys, "tenant_")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if want := "CREATE TABLE tenant_users (id bigint);"; files[0].SQL != want {
		t.Fatalf("Load() SQL = %q, want %q", files[0].SQL, want)
	}
}

func TestLoadRejectsUnsafePrefix(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"0001_init.sql": {Data: []byte("SELECT 1;")}}
	for _, prefix := range []string{"Base_", "base-", "base_; DROP TABLE users; --", "1base_"} {
		if _, err := Load(fsys, prefix); err == nil {
			t.Fatalf("Load() accepted unsafe prefix %q", prefix)
		}
	}
}

func TestLoadAcceptsEmptyPrefix(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"0001_init.sql": {Data: []byte("CREATE TABLE {{prefix}}users (id bigint);")}}
	files, err := Load(fsys, "")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if want := "CREATE TABLE users (id bigint);"; files[0].SQL != want {
		t.Fatalf("Load() SQL = %q, want %q", files[0].SQL, want)
	}
}

func TestEmbeddedMigrationsAreValid(t *testing.T) {
	t.Parallel()

	files, err := Load(migrations.FS, "base_")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("embedded migrations = %d, want at least 2", len(files))
	}
	for _, file := range files {
		if strings.TrimSpace(file.SQL) == "" {
			t.Fatalf("migration %s is empty", file.Name)
		}
		if strings.Contains(file.SQL, PrefixPlaceholder) {
			t.Fatalf("migration %s still contains the prefix placeholder", file.Name)
		}
		if !strings.Contains(file.SQL, "base_") {
			t.Fatalf("migration %s does not reference prefixed tables", file.Name)
		}
	}
}
