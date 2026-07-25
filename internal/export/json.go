// Package export produces the two Phase 1 export forms — the full JSON
// export conforming to inventory.schema.json, and (in exportstatic.go, step
// 7) the offline static HTML mirror — and applies the sensitivity policy
// from docs/threat-model.md before anything leaves the instance.
//
// The policy, by field classification rather than pattern matching:
//
//   - Documents of kind identity, financial, insurance, or medical NEVER
//     appear in any export, under any option. There is no flag for them; the
//     SQLite file itself is the only place they live until the Phase 2
//     encrypted vault exists.
//   - Other documents are gated on their own export_ok, defaulting false.
//     There is deliberately no bulk override.
//   - person.needs, item.serial, item.cost, and container.location_note are
//     stripped by default; IncludeSensitive restores them for a local
//     backup, and the CLI warns loudly about exactly what that includes.
package export

import (
	"encoding/json"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
)

// Options control how much a JSON export carries.
type Options struct {
	// IncludeSensitive restores the default-stripped fields (needs, serial,
	// cost, location_note) and includes documents regardless of export_ok.
	// It exists for local backup. It does NOT include never-export document
	// kinds; nothing does.
	IncludeSensitive bool
}

// Filter returns a copy of inv with the sensitivity policy applied.
func Filter(inv model.Inventory, opts Options) model.Inventory {
	out := inv

	if !opts.IncludeSensitive {
		if inv.Household != nil {
			h := *inv.Household
			h.Persons = make([]model.Person, len(inv.Household.Persons))
			for i, p := range inv.Household.Persons {
				p.Needs = nil
				h.Persons[i] = p
			}
			out.Household = &h
		}
		out.Items = make([]model.Item, len(inv.Items))
		for i, it := range inv.Items {
			it.Serial = ""
			it.Cost = nil
			out.Items[i] = it
		}
		out.Containers = make([]model.Container, len(inv.Containers))
		for i, c := range inv.Containers {
			c.LocationNote = ""
			out.Containers[i] = c
		}
	}

	out.Documents = nil
	for _, d := range inv.Documents {
		if d.Kind.NeverExportUnencrypted() {
			continue
		}
		if !opts.IncludeSensitive && !d.ExportOK {
			continue
		}
		out.Documents = append(out.Documents, d)
	}

	return out
}

// JSON stamps exported_at and marshals the filtered inventory, indented for
// human inspection. The caller supplies the clock.
func JSON(inv model.Inventory, now time.Time, opts Options) ([]byte, error) {
	filtered := Filter(inv, opts)
	filtered.ExportedAt = now.UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(&filtered, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
