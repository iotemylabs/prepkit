package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/iotemylabs/prepkit/internal/model"
)

const kitCols = `id, name, template_id, template_version, layer, duration, evacuation_tier, container_id, assigned_person_id, last_verified, notes`

func validateKit(k *model.Kit) error {
	if k.Name == "" {
		return vErrf("kit name is required")
	}
	if k.Layer != "" && !k.Layer.Valid() {
		return vErrf("invalid layer %q", k.Layer)
	}
	if k.Duration != "" && !k.Duration.Valid() {
		return vErrf("invalid duration %q", k.Duration)
	}
	if k.EvacuationTier != "" && !k.EvacuationTier.Valid() {
		return vErrf("invalid evacuation_tier %q", k.EvacuationTier)
	}
	return validDateField("last_verified", k.LastVerified)
}

func (s *Store) CreateKit(ctx context.Context, k *model.Kit) error {
	if k.ID == "" {
		k.ID = NewID()
	}
	if err := validateKit(k); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kits (`+kitCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, nz(k.TemplateID), nz(k.TemplateVersion), nz(string(k.Layer)),
		nz(string(k.Duration)), nz(string(k.EvacuationTier)), nz(k.ContainerID),
		nz(k.AssignedPersonID), nz(k.LastVerified), nz(k.Notes))
	return err
}

func (s *Store) UpdateKit(ctx context.Context, k *model.Kit) error {
	if err := validateKit(k); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE kits SET name = ?, template_id = ?, template_version = ?, layer = ?,
			duration = ?, evacuation_tier = ?, container_id = ?, assigned_person_id = ?,
			last_verified = ?, notes = ? WHERE id = ?`,
		k.Name, nz(k.TemplateID), nz(k.TemplateVersion), nz(string(k.Layer)),
		nz(string(k.Duration)), nz(string(k.EvacuationTier)), nz(k.ContainerID),
		nz(k.AssignedPersonID), nz(k.LastVerified), nz(k.Notes), k.ID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

// DeleteKit removes the kit; its items survive with kit_id cleared (the
// schema's ON DELETE SET NULL) rather than being destroyed with it.
func (s *Store) DeleteKit(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM kits WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) GetKit(ctx context.Context, id string) (*model.Kit, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+kitCols+` FROM kits WHERE id = ?`, id)
	return scanKit(row)
}

func (s *Store) ListKits(ctx context.Context) ([]model.Kit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+kitCols+` FROM kits ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Kit
	for rows.Next() {
		k, err := scanKit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func scanKit(row scanner) (*model.Kit, error) {
	var k model.Kit
	var tid, tver, layer, dur, evac, cont, person, verified, notes sql.NullString
	if err := row.Scan(&k.ID, &k.Name, &tid, &tver, &layer, &dur, &evac, &cont, &person, &verified, &notes); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	k.TemplateID = str(tid)
	k.TemplateVersion = str(tver)
	k.Layer = model.Layer(str(layer))
	k.Duration = model.Duration(str(dur))
	k.EvacuationTier = model.EvacuationTier(str(evac))
	k.ContainerID = str(cont)
	k.AssignedPersonID = str(person)
	k.LastVerified = str(verified)
	k.Notes = str(notes)
	return &k, nil
}
