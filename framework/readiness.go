package framework

import (
	"github.com/tjahjono/gostack/database"
	"github.com/tjahjono/gostack/internal/bootstrap"
	"github.com/tjahjono/gostack/migration"
)

// NewReadiness supports applications that explicitly construct their own pool,
// repositories, services, deliveries, and module descriptors before creating App.
func NewReadiness(db database.Client, verifier migration.Verifier) (Readiness, error) {
	return bootstrap.NewReadiness(db, verifier)
}
