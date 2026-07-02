package native

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rxttd/cheron/internal/adapter"
	"github.com/rxttd/cheron/internal/event"
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

func init() { adapter.Register(New()) }

func (Adapter) Name() string { return "native" }
func (Adapter) Path() string { return "/ingest" }

// Match accepts a single Event object or a JSON array of them (grafana can batch
// several alerts into one webhook POST), and returns one Event per alert.
func (Adapter) Match(r *http.Request) ([]event.Event, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
	}
	var evs []event.Event
	if t := bytes.TrimLeft(body, " \t\r\n"); len(t) > 0 && t[0] == '[' {
		if err := json.Unmarshal(body, &evs); err != nil {
			return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
		}
	} else {
		var ev event.Event
		if err := json.Unmarshal(body, &ev); err != nil {
			return nil, fmt.Errorf("%w: %w", adapter.ErrNotMatched, err)
		}
		evs = []event.Event{ev}
	}
	if len(evs) == 0 {
		return nil, fmt.Errorf("%w: empty batch", adapter.ErrNotMatched)
	}
	for i := range evs {
		if evs[i].Title == "" || evs[i].Channel == "" || evs[i].Status == "" {
			return nil, fmt.Errorf("%w: missing title/channel/status", adapter.ErrNotMatched)
		}
		evs[i].Normalize()
	}
	return evs, nil
}
