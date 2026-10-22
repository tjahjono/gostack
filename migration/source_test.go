package migration_test

import (
	"testing"
	"testing/fstest"

	"github.com/tjahjono/gostack/migration"
)

func TestSourceValidation(t *testing.T) {
	for _, test := range []struct {
		name  string
		files fstest.MapFS
		valid bool
	}{
		{"paired", fstest.MapFS{"framework/20261022000000_test.up.sql": {Data: []byte("SELECT 1;")}, "framework/20261022000000_test.down.sql": {Data: []byte("SELECT 2;")}, "app/README.md": {Data: []byte("empty")}}, true},
		{"unpaired", fstest.MapFS{"framework/20261022000000_test.up.sql": {Data: []byte("SELECT 1;")}, "app/README.md": {Data: []byte("empty")}}, false},
		{"malformed", fstest.MapFS{"framework/nope.sql": {Data: []byte("SELECT 1;")}, "app/README.md": {Data: []byte("empty")}}, false},
		{"invalid date", fstest.MapFS{"framework/20269922000000_test.up.sql": {Data: []byte("SELECT 1;")}, "framework/20269922000000_test.down.sql": {Data: []byte("SELECT 2;")}, "app/README.md": {Data: []byte("empty")}}, false},
		{"missing stream", fstest.MapFS{"framework/README.md": {Data: []byte("empty")}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := migration.NewFileSource(test.files)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v: %v", test.valid, err)
			}
		})
	}
}

func TestChecksumIncludesRollback(t *testing.T) {
	files := fstest.MapFS{"framework/20261022000000_test.up.sql": {Data: []byte("SELECT 1;")}, "framework/20261022000000_test.down.sql": {Data: []byte("SELECT 2;")}, "app/README.md": {Data: []byte("empty")}}
	s, err := migration.NewFileSource(files)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Load()
	files["framework/20261022000000_test.down.sql"].Data = []byte("SELECT 3;")
	after, _ := s.Load()
	if before["framework"][0].Checksum == after["framework"][0].Checksum {
		t.Fatal("rollback change was not detected")
	}
}
