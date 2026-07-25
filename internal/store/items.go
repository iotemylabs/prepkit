package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/iotemylabs/prepkit/internal/model"
)

const itemCols = `id, name, category, template_item_id, kit_id, container_id, quantity, unit, product, weight_g, acquired, cost, photo_ids_json, serial, notes, archived`

func validateItem(it *model.Item) error {
	if it.Name == "" {
		return vErrf("item name is required")
	}
	if !it.Category.Valid() {
		return vErrf("invalid category %q", it.Category)
	}
	if it.Quantity != nil && *it.Quantity < 0 {
		return vErrf("quantity must be >= 0")
	}
	if it.WeightG != nil && *it.WeightG < 0 {
		return vErrf("weight_g must be >= 0")
	}
	if it.Cost != nil && *it.Cost < 0 {
		return vErrf("cost must be >= 0")
	}
	return validDateField("acquired", it.Acquired)
}

func photoIDsToJSON(ids []string) (any, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (s *Store) CreateItem(ctx context.Context, it *model.Item) error {
	if it.ID == "" {
		it.ID = NewID()
	}
	if err := validateItem(it); err != nil {
		return err
	}
	photos, err := photoIDsToJSON(it.PhotoIDs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO items (`+itemCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.Name, string(it.Category), nz(it.TemplateItemID), nz(it.KitID),
		nz(it.ContainerID), nzf(it.Quantity), nz(it.Unit), nz(it.Product),
		nzf(it.WeightG), nz(it.Acquired), nzf(it.Cost), photos, nz(it.Serial),
		nz(it.Notes), boolInt(it.Archived))
	return err
}

// UpdateItem replaces the item. Setting a quantity on an item that has lots
// is rejected: the effective quantity of a lotted item is the sum of its
// lots, and letting both exist is how the two come to disagree.
func (s *Store) UpdateItem(ctx context.Context, it *model.Item) error {
	if err := validateItem(it); err != nil {
		return err
	}
	photos, err := photoIDsToJSON(it.PhotoIDs)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if it.Quantity != nil {
			var lots int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM lots WHERE item_id = ?`, it.ID).Scan(&lots); err != nil {
				return err
			}
			if lots > 0 {
				return fmt.Errorf("item %s: %w", it.ID, ErrHasLots)
			}
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE items SET name = ?, category = ?, template_item_id = ?, kit_id = ?,
				container_id = ?, quantity = ?, unit = ?, product = ?, weight_g = ?,
				acquired = ?, cost = ?, photo_ids_json = ?, serial = ?, notes = ?,
				archived = ? WHERE id = ?`,
			it.Name, string(it.Category), nz(it.TemplateItemID), nz(it.KitID),
			nz(it.ContainerID), nzf(it.Quantity), nz(it.Unit), nz(it.Product),
			nzf(it.WeightG), nz(it.Acquired), nzf(it.Cost), photos, nz(it.Serial),
			nz(it.Notes), boolInt(it.Archived), it.ID)
		if err != nil {
			return err
		}
		return mustAffect(res)
	})
}

// DeleteItem removes the item; its lots cascade with it.
func (s *Store) DeleteItem(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) GetItem(ctx context.Context, id string) (*model.Item, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM items WHERE id = ?`, id)
	return scanItem(row)
}

func (s *Store) ListItems(ctx context.Context) ([]model.Item, error) {
	return s.listItems(ctx, `SELECT `+itemCols+` FROM items ORDER BY name, id`)
}

func (s *Store) ListItemsByKit(ctx context.Context, kitID string) ([]model.Item, error) {
	return s.listItems(ctx, `SELECT `+itemCols+` FROM items WHERE kit_id = ? ORDER BY name, id`, kitID)
}

func (s *Store) listItems(ctx context.Context, query string, args ...any) ([]model.Item, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

func scanItem(row scanner) (*model.Item, error) {
	var it model.Item
	var cat string
	var tmpl, kit, cont, unit, product, acquired, photos, serial, notes sql.NullString
	var qty, weight, cost sql.NullFloat64
	var archived int
	if err := row.Scan(&it.ID, &it.Name, &cat, &tmpl, &kit, &cont, &qty, &unit,
		&product, &weight, &acquired, &cost, &photos, &serial, &notes, &archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	it.Category = model.Category(cat)
	it.TemplateItemID = str(tmpl)
	it.KitID = str(kit)
	it.ContainerID = str(cont)
	it.Quantity = fptr(qty)
	it.Unit = str(unit)
	it.Product = str(product)
	it.WeightG = fptr(weight)
	it.Acquired = str(acquired)
	it.Cost = fptr(cost)
	it.Serial = str(serial)
	it.Notes = str(notes)
	it.Archived = archived != 0
	if photos.Valid && photos.String != "" {
		if err := json.Unmarshal([]byte(photos.String), &it.PhotoIDs); err != nil {
			return nil, err
		}
	}
	return &it, nil
}

// --- Lots ---------------------------------------------------------------

const lotCols = `id, item_id, quantity, unit, expires, expiry_basis, opened, next_check, container_id, status, notes`

func validateLot(l *model.Lot) error {
	if l.ItemID == "" {
		return vErrf("lot item_id is required")
	}
	if l.Quantity < 0 {
		return vErrf("quantity must be >= 0")
	}
	if l.Status != "" && !l.Status.Valid() {
		return vErrf("invalid lot status %q", l.Status)
	}
	if l.ExpiryBasis != "" && !l.ExpiryBasis.Valid() {
		return vErrf("invalid expiry_basis %q", l.ExpiryBasis)
	}
	for _, d := range []struct{ field, val string }{
		{"expires", l.Expires}, {"opened", l.Opened}, {"next_check", l.NextCheck},
	} {
		if err := validDateField(d.field, d.val); err != nil {
			return err
		}
	}
	return nil
}

// CreateLot inserts a lot and, in the same transaction, clears the parent
// item's own quantity: from the first lot onward the item's effective
// quantity is derived by summing lots, so the column must be NULL.
func (s *Store) CreateLot(ctx context.Context, l *model.Lot) error {
	if l.ID == "" {
		l.ID = NewID()
	}
	if err := validateLot(l); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM items WHERE id = ?`, l.ItemID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("item %s: %w", l.ItemID, ErrNotFound)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO lots (`+lotCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ID, l.ItemID, l.Quantity, nz(l.Unit), nz(l.Expires),
			nz(string(l.ExpiryBasis)), nz(l.Opened), nz(l.NextCheck),
			nz(l.ContainerID), string(l.EffectiveStatus()), nz(l.Notes)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`UPDATE items SET quantity = NULL WHERE id = ?`, l.ItemID)
		return err
	})
}

func (s *Store) UpdateLot(ctx context.Context, l *model.Lot) error {
	if err := validateLot(l); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE lots SET item_id = ?, quantity = ?, unit = ?, expires = ?,
			expiry_basis = ?, opened = ?, next_check = ?, container_id = ?,
			status = ?, notes = ? WHERE id = ?`,
		l.ItemID, l.Quantity, nz(l.Unit), nz(l.Expires), nz(string(l.ExpiryBasis)),
		nz(l.Opened), nz(l.NextCheck), nz(l.ContainerID),
		string(l.EffectiveStatus()), nz(l.Notes), l.ID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) DeleteLot(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM lots WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) GetLot(ctx context.Context, id string) (*model.Lot, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+lotCols+` FROM lots WHERE id = ?`, id)
	return scanLot(row)
}

func (s *Store) ListLotsByItem(ctx context.Context, itemID string) ([]model.Lot, error) {
	return s.listLots(ctx,
		`SELECT `+lotCols+` FROM lots WHERE item_id = ? ORDER BY expires IS NULL, expires, id`, itemID)
}

func (s *Store) ListLots(ctx context.Context) ([]model.Lot, error) {
	return s.listLots(ctx,
		`SELECT `+lotCols+` FROM lots ORDER BY expires IS NULL, expires, id`)
}

func (s *Store) listLots(ctx context.Context, query string, args ...any) ([]model.Lot, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Lot
	for rows.Next() {
		l, err := scanLot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func scanLot(row scanner) (*model.Lot, error) {
	var l model.Lot
	var unit, expires, basis, opened, next, cont, notes sql.NullString
	var status string
	if err := row.Scan(&l.ID, &l.ItemID, &l.Quantity, &unit, &expires, &basis,
		&opened, &next, &cont, &status, &notes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	l.Unit = str(unit)
	l.Expires = str(expires)
	l.ExpiryBasis = model.ExpiryBasis(str(basis))
	l.Opened = str(opened)
	l.NextCheck = str(next)
	l.ContainerID = str(cont)
	l.Status = model.LotStatus(status)
	l.Notes = str(notes)
	return &l, nil
}
