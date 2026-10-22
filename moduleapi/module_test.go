package moduleapi_test

import (
	"github.com/tjahjono/gostack/moduleapi"
	"testing"
)

func TestDescriptorOwnsMetadata(t *testing.T) {
	deps := []string{"first"}
	permissions := []string{"item:read"}
	d, err := moduleapi.NewDescriptor("sample", deps, permissions)
	if err != nil {
		t.Fatal(err)
	}
	deps[0] = "changed"
	permissions[0] = "changed:read"
	snapshot := d.Dependencies()
	snapshot[0] = "also_changed"
	if d.Dependencies()[0] != "first" || d.Permissions()[0] != "item:read" {
		t.Fatal("descriptor aliases caller memory")
	}
	if _, err := moduleapi.NewDescriptor("sample", []string{"sample"}, nil); err == nil {
		t.Fatal("self dependency accepted")
	}
}
