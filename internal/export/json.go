// Package export produces the two Phase 1 export forms — the full JSON
// export conforming to inventory.schema.json, and (in static.go) the offline
// static HTML mirror — and applies the sensitivity policy from
// docs/threat-model.md before anything leaves the instance.
//
// The policy, by field classification rather than pattern matching:
//
//   - Documents of kind identity, financial, insurance, or medical NEVER
//     appear in any export. The SQLite file itself is the only place they
//     live until the Phase 2 encrypted vault exists.
//   - Other documents are gated on their own export_ok, defaulting false.
//     There is deliberately no bulk override.
//   - person.needs, item.serial, item.cost, and container.location_note are
//     always stripped. There is deliberately no option to restore them: a
//     complete copy of the data is a backup of the database (store.Backup),
//     not an export.
package export

import (
	"encoding/json"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
)

// Filter returns a copy of inv with the sensitivity policy applied.
func Filter(inv model.Inventory) model.Inventory {
	out := inv

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

	out.Documents = nil
	for _, d := range inv.Documents {
		if d.Kind.NeverExportUnencrypted() || !d.ExportOK {
			continue
		}
		out.Documents = append(out.Documents, d)
	}

	return out
}

// JSON stamps exported_at and marshals the filtered inventory, indented for
// human inspection. The caller supplies the clock.
func JSON(inv model.Inventory, now time.Time) ([]byte, error) {
	filtered := Filter(inv)
	filtered.ExportedAt = now.UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(&filtered, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
