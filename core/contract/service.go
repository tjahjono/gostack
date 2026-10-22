package contract

import (
	"context"
	"github.com/tjahjono/gostack/core"
	"github.com/tjahjono/gostack/security"
)

type Service[ID comparable, Entity, Create, Update, Filter any] interface {
	Create(context.Context, security.Principal, Create) (Entity, error)
	Update(context.Context, security.Principal, ID, Update) (Entity, error)
	Delete(context.Context, security.Principal, ID, uint64) error
	GetByID(context.Context, security.Principal, ID) (Entity, error)
	List(context.Context, security.Principal, Filter, core.PageRequest) (core.Page[Entity], error)
}
