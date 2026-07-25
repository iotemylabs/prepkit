package store

import (
	"context"

	"github.com/iotemylabs/prepkit/internal/model"
)

// Snapshot reads the entire database into an Inventory. It is the raw,
// unredacted view: the export layer applies the sensitivity policy before
// anything leaves the instance.
func (s *Store) Snapshot(ctx context.Context) (*model.Inventory, error) {
	inv := &model.Inventory{SpecVersion: model.SpecVersion}
	var err error
	if inv.InstanceID, err = s.InstanceID(ctx); err != nil {
		return nil, err
	}
	h, err := s.GetHousehold(ctx)
	if err != nil {
		return nil, err
	}
	if h.Label != "" || len(h.Persons) > 0 || h.Pets != 0 || h.Climate != "" || h.Environment != "" {
		inv.Household = h
	}
	if inv.Containers, err = s.ListContainers(ctx); err != nil {
		return nil, err
	}
	if inv.Kits, err = s.ListKits(ctx); err != nil {
		return nil, err
	}
	if inv.Items, err = s.ListItems(ctx); err != nil {
		return nil, err
	}
	if inv.Lots, err = s.ListLots(ctx); err != nil {
		return nil, err
	}
	if inv.Documents, err = s.ListDocuments(ctx); err != nil {
		return nil, err
	}
	if inv.Maintenance, err = s.ListMaintenance(ctx); err != nil {
		return nil, err
	}
	return inv, nil
}

// Empty reports whether the database holds no user data yet. Import requires
// an empty database: merging two inventories has no well-defined answer for
// conflicting ids, so it is refused rather than guessed at.
func (s *Store) Empty(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM household)
		     + (SELECT COUNT(*) FROM persons)
		     + (SELECT COUNT(*) FROM containers)
		     + (SELECT COUNT(*) FROM kits)
		     + (SELECT COUNT(*) FROM items)
		     + (SELECT COUNT(*) FROM lots)
		     + (SELECT COUNT(*) FROM documents)
		     + (SELECT COUNT(*) FROM maintenance_events)`).Scan(&n)
	return n == 0, err
}
