package export

import (
	"context"
	"fmt"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/internal/store"
)

// Import writes a full inventory export into an empty database, preserving
// every id so links survive. The target keeps its own instance_id:
// instance_id identifies an installation, not the data.
//
// Import refuses a non-empty database — merging two inventories has no
// well-defined answer for conflicting ids — and refuses a spec_version it
// does not implement rather than reinterpreting it.
func Import(ctx context.Context, st *store.Store, inv *model.Inventory) error {
	if inv.SpecVersion != model.SpecVersion {
		return fmt.Errorf("export declares spec_version %q; this build implements %q", inv.SpecVersion, model.SpecVersion)
	}
	empty, err := st.Empty(ctx)
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("import requires an empty database; this one already holds data")
	}

	if inv.Household != nil {
		if err := st.UpdateHousehold(ctx, inv.Household); err != nil {
			return fmt.Errorf("household: %w", err)
		}
		for i := range inv.Household.Persons {
			p := inv.Household.Persons[i]
			if err := st.CreatePerson(ctx, &p); err != nil {
				return fmt.Errorf("person %s: %w", p.ID, err)
			}
		}
	}

	// Containers reference their parents, so insert in dependency order:
	// repeatedly take every container whose parent is already in. No
	// progress with containers left means a missing parent or a cycle,
	// either of which the source data should never contain.
	inserted := map[string]bool{}
	remaining := append([]model.Container(nil), inv.Containers...)
	for len(remaining) > 0 {
		var next []model.Container
		progress := false
		for _, c := range remaining {
			if c.ParentID == "" || inserted[c.ParentID] {
				cc := c
				if err := st.CreateContainer(ctx, &cc); err != nil {
					return fmt.Errorf("container %s: %w", c.ID, err)
				}
				inserted[c.ID] = true
				progress = true
			} else {
				next = append(next, c)
			}
		}
		if !progress {
			return fmt.Errorf("containers with unresolvable parents (missing parent or cycle): %d remaining", len(next))
		}
		remaining = next
	}

	for i := range inv.Kits {
		k := inv.Kits[i]
		if err := st.CreateKit(ctx, &k); err != nil {
			return fmt.Errorf("kit %s: %w", k.ID, err)
		}
	}
	for i := range inv.Items {
		it := inv.Items[i]
		if err := st.CreateItem(ctx, &it); err != nil {
			return fmt.Errorf("item %s: %w", it.ID, err)
		}
	}
	for i := range inv.Lots {
		l := inv.Lots[i]
		if err := st.CreateLot(ctx, &l); err != nil {
			return fmt.Errorf("lot %s: %w", l.ID, err)
		}
	}
	for i := range inv.Documents {
		d := inv.Documents[i]
		if err := st.CreateDocument(ctx, &d); err != nil {
			return fmt.Errorf("document %s: %w", d.ID, err)
		}
	}
	for i := range inv.Maintenance {
		m := inv.Maintenance[i]
		if err := st.CreateMaintenance(ctx, &m); err != nil {
			return fmt.Errorf("maintenance event %s: %w", m.ID, err)
		}
	}
	return nil
}
