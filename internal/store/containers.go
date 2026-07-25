package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/iotemylabs/prepkit/internal/model"
)

const containerCols = `id, name, parent_id, type, color_code, location_note, qr_slug, weight_g`

func validateContainer(c *model.Container) error {
	if c.Name == "" {
		return vErrf("container name is required")
	}
	if c.Type != "" && !c.Type.Valid() {
		return vErrf("invalid container type %q", c.Type)
	}
	if c.WeightG != nil && *c.WeightG < 0 {
		return vErrf("weight_g must be >= 0")
	}
	return nil
}

// CreateContainer inserts a container. If no QR slug is supplied one is
// generated server-side: slugs are always opaque local tokens, never
// user-meaningful strings that might leak location or contents onto a
// printed label.
func (s *Store) CreateContainer(ctx context.Context, c *model.Container) error {
	if c.ID == "" {
		c.ID = NewID()
	}
	if err := validateContainer(c); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := checkNoCycle(ctx, tx, c.ID, c.ParentID); err != nil {
			return err
		}
		if c.QRSlug == "" {
			slug, err := freshSlug(ctx, tx)
			if err != nil {
				return err
			}
			c.QRSlug = slug
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO containers (`+containerCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.Name, nz(c.ParentID), nz(string(c.Type)), nz(c.ColorCode),
			nz(c.LocationNote), nz(c.QRSlug), nzf(c.WeightG))
		return err
	})
}

func (s *Store) UpdateContainer(ctx context.Context, c *model.Container) error {
	if err := validateContainer(c); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := checkNoCycle(ctx, tx, c.ID, c.ParentID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE containers SET name = ?, parent_id = ?, type = ?, color_code = ?,
				location_note = ?, qr_slug = ?, weight_g = ? WHERE id = ?`,
			c.Name, nz(c.ParentID), nz(string(c.Type)), nz(c.ColorCode),
			nz(c.LocationNote), nz(c.QRSlug), nzf(c.WeightG), c.ID)
		if err != nil {
			return err
		}
		return mustAffect(res)
	})
}

// DeleteContainer refuses to delete a container that still has children or
// contents; emptying it first is a deliberate act the user must take.
func (s *Store) DeleteContainer(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var refs int
		err := tx.QueryRowContext(ctx, `
			SELECT (SELECT COUNT(*) FROM containers WHERE parent_id = ?)
			     + (SELECT COUNT(*) FROM items WHERE container_id = ?)
			     + (SELECT COUNT(*) FROM lots WHERE container_id = ?)
			     + (SELECT COUNT(*) FROM kits WHERE container_id = ?)`,
			id, id, id, id).Scan(&refs)
		if err != nil {
			return err
		}
		if refs > 0 {
			return fmt.Errorf("container %s: %w", id, ErrInUse)
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM containers WHERE id = ?`, id)
		if err != nil {
			return err
		}
		return mustAffect(res)
	})
}

func (s *Store) GetContainer(ctx context.Context, id string) (*model.Container, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+containerCols+` FROM containers WHERE id = ?`, id)
	return scanContainer(row)
}

// GetContainerByQRSlug resolves a scanned label to its container.
func (s *Store) GetContainerByQRSlug(ctx context.Context, slug string) (*model.Container, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+containerCols+` FROM containers WHERE qr_slug = ?`, slug)
	return scanContainer(row)
}

func (s *Store) ListContainers(ctx context.Context) ([]model.Container, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+containerCols+` FROM containers ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Container
	for rows.Next() {
		c, err := scanContainer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func scanContainer(row scanner) (*model.Container, error) {
	var c model.Container
	var parent, typ, color, loc, slug sql.NullString
	var weight sql.NullFloat64
	if err := row.Scan(&c.ID, &c.Name, &parent, &typ, &color, &loc, &slug, &weight); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.ParentID = str(parent)
	c.Type = model.ContainerType(str(typ))
	c.ColorCode = str(color)
	c.LocationNote = str(loc)
	c.QRSlug = str(slug)
	c.WeightG = fptr(weight)
	return &c, nil
}

// checkNoCycle walks the ancestor chain from parentID; encountering id means
// the write would create a cycle (self-parentage is the depth-0 case). Runs
// inside the writing transaction so a concurrent edit cannot slip a cycle in
// between check and write.
func checkNoCycle(ctx context.Context, tx *sql.Tx, id, parentID string) error {
	seen := map[string]bool{}
	cur := parentID
	for cur != "" {
		if cur == id {
			return ErrContainerCycle
		}
		if seen[cur] {
			// A pre-existing cycle not involving id; refuse to build on it.
			return ErrContainerCycle
		}
		seen[cur] = true
		var next sql.NullString
		err := tx.QueryRowContext(ctx,
			`SELECT parent_id FROM containers WHERE id = ?`, cur).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			return vErrf("parent container %s does not exist", cur)
		}
		if err != nil {
			return err
		}
		cur = str(next)
	}
	return nil
}

// freshSlug generates QR slugs until one is unused. Collisions are
// vanishingly rare (40 random bits), so this effectively runs once.
func freshSlug(ctx context.Context, tx *sql.Tx) (string, error) {
	for {
		slug := NewQRSlug()
		var n int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM containers WHERE qr_slug = ?`, slug).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return slug, nil
		}
	}
}
