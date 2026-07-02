package adapter

import (
	"errors"
	"net/http"

	"github.com/rxttd/cheron/internal/event"
)

var ErrNotMatched = errors.New("adapter did not match the request body")

// Adapter turns a request already routed to its Path into one or more Events.
type Adapter interface {
	Name() string
	Path() string
	Match(r *http.Request) ([]event.Event, error)
}

var registry []Adapter

// Register is called from an adapter package's init.
func Register(a Adapter) { registry = append(registry, a) }

func Registered() []Adapter { return registry }
