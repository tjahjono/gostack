package security_test

import (
	"context"
	"testing"

	"github.com/tjahjono/gostack/security"
)

func TestVerifiedIdentityValues(t *testing.T) {
	if (security.Principal{}).Valid() {
		t.Fatal("empty principal valid")
	}
	if _, err := security.NewPrincipal("bad\nsubject", "session", security.Primary); err == nil {
		t.Fatal("invalid identity accepted")
	}
	p, err := security.NewPrincipal("user-1", "session-1", security.MultiFactor)
	if err != nil {
		t.Fatal(err)
	}
	ctx := security.WithPrincipal(context.Background(), p)
	actual, ok := security.PrincipalFrom(ctx)
	if !ok || actual.SubjectID() != "user-1" || actual.Assurance() != security.MultiFactor {
		t.Fatal("identity context failed")
	}
}
