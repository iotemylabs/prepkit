package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/iotemylabs/prepkit/internal/model"
)

// GetHousehold returns the household profile with persons populated. A
// never-configured household comes back empty rather than as an error.
func (s *Store) GetHousehold(ctx context.Context) (*model.Household, error) {
	h := &model.Household{}
	var label, climate, environment sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT label, pets, climate, environment FROM household WHERE id = 1`).
		Scan(&label, &h.Pets, &climate, &environment)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	h.Label = str(label)
	h.Climate = model.Climate(str(climate))
	h.Environment = model.Environment(str(environment))

	persons, err := s.ListPersons(ctx)
	if err != nil {
		return nil, err
	}
	h.Persons = persons
	return h, nil
}

// UpdateHousehold upserts the single household row. Persons are managed via
// their own CRUD and ignored here.
func (s *Store) UpdateHousehold(ctx context.Context, h *model.Household) error {
	if h.Pets < 0 {
		return vErrf("pets must be >= 0")
	}
	// "any" is a template assumption, not a place a household can live.
	if h.Climate != "" && (h.Climate == model.ClimateAny || !h.Climate.Valid()) {
		return vErrf("invalid household climate %q", h.Climate)
	}
	if h.Environment != "" && (h.Environment == model.EnvAny || !h.Environment.Valid()) {
		return vErrf("invalid household environment %q", h.Environment)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO household (id, label, pets, climate, environment) VALUES (1, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET label = excluded.label, pets = excluded.pets,
			climate = excluded.climate, environment = excluded.environment`,
		nz(h.Label), h.Pets, nz(string(h.Climate)), nz(string(h.Environment)))
	return err
}

func validatePerson(p *model.Person) error {
	if p.Label == "" {
		return vErrf("person label is required")
	}
	if p.AgeBand != "" && !p.AgeBand.Valid() {
		return vErrf("invalid age_band %q", p.AgeBand)
	}
	return nil
}

// needsToJSON serializes the SENSITIVE needs list; NULL when empty.
func needsToJSON(needs []string) (any, error) {
	if len(needs) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(needs)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func needsFromJSON(ns sql.NullString) ([]string, error) {
	if !ns.Valid || ns.String == "" {
		return nil, nil
	}
	var needs []string
	if err := json.Unmarshal([]byte(ns.String), &needs); err != nil {
		return nil, err
	}
	return needs, nil
}

func (s *Store) CreatePerson(ctx context.Context, p *model.Person) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	if err := validatePerson(p); err != nil {
		return err
	}
	needs, err := needsToJSON(p.Needs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO persons (id, label, age_band, needs_json) VALUES (?, ?, ?, ?)`,
		p.ID, p.Label, nz(string(p.AgeBand)), needs)
	return err
}

func (s *Store) UpdatePerson(ctx context.Context, p *model.Person) error {
	if err := validatePerson(p); err != nil {
		return err
	}
	needs, err := needsToJSON(p.Needs)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE persons SET label = ?, age_band = ?, needs_json = ? WHERE id = ?`,
		p.Label, nz(string(p.AgeBand)), needs, p.ID)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) DeletePerson(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM persons WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) GetPerson(ctx context.Context, id string) (*model.Person, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, label, age_band, needs_json FROM persons WHERE id = ?`, id)
	return scanPerson(row)
}

func (s *Store) ListPersons(ctx context.Context) ([]model.Person, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, age_band, needs_json FROM persons ORDER BY label, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Person
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPerson(row scanner) (*model.Person, error) {
	var p model.Person
	var ageBand, needs sql.NullString
	if err := row.Scan(&p.ID, &p.Label, &ageBand, &needs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.AgeBand = model.AgeBand(str(ageBand))
	var err error
	if p.Needs, err = needsFromJSON(needs); err != nil {
		return nil, err
	}
	return &p, nil
}

// mustAffect converts a zero-row UPDATE/DELETE into ErrNotFound.
func mustAffect(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
