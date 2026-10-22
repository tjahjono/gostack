// Package contract contains optional typed building blocks for module contracts.
// Modules define named, use-case-specific interfaces and may embed these shapes.
package contract

import (
	"context"
	"github.com/tjahjono/gostack/core"
)

type Repository[ID comparable, Entity, Create, Update, Filter any] interface {
	Create(context.Context, Create) (Entity, error)
	Update(context.Context, ID, Update) (Entity, error)
	Delete(context.Context, ID, uint64) error
	GetByID(context.Context, ID) (Entity, error)
	List(context.Context, Filter, core.PageRequest) (core.Page[Entity], error)
}

// Transaction supplies operation-specific repository interfaces, never SQL handles.
// Callbacks must honor context, contain no external effects, and must not retain R.
type Transaction[R any] interface {
	Within(context.Context, func(R) error) error
}
