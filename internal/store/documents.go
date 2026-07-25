package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/iotemylabs/prepkit/internal/model"
)

const documentCols = `id, title, kind, body, file_id, review_interval_months, last_reviewed, export_ok`

func validateDocument(d *model.Document) error {
	if d.Title == "" {
		return vErrf("document title is required")
	}
	if !d.Kind.Valid() {
		return vErrf("invalid document kind %q", d.Kind)
	}
	if d.ReviewIntervalMonths < 0 {
		return vErrf("review_interval_months must be > 0 when set")
	}
	return validDateField("last_reviewed", d.LastReviewed)
}

func (s *Store) CreateDocument(ctx context.Context, d *model.Document) error {
	if d.ID == "" {
		d.ID = NewID()
	}
	if err := validateDocument(d); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO documents (`+documentCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.Title, string(d.Kind), nz(d.Body), nz(d.FileID),
			nzUnsetF(d.ReviewIntervalMonths), nz(d.LastReviewed), boolInt(d.ExportOK)); err != nil {
			return err
		}
		return syncDocumentPersons(ctx, tx, d.ID, d.PersonIDs)
	})
}

func (s *Store) UpdateDocument(ctx context.Context, d *model.Document) error {
	if err := validateDocument(d); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE documents SET title = ?, kind = ?, body = ?, file_id = ?,
				review_interval_months = ?, last_reviewed = ?, export_ok = ? WHERE id = ?`,
			d.Title, string(d.Kind), nz(d.Body), nz(d.FileID),
			nzUnsetF(d.ReviewIntervalMonths), nz(d.LastReviewed), boolInt(d.ExportOK), d.ID)
		if err != nil {
			return err
		}
		if err := mustAffect(res); err != nil {
			return err
		}
		return syncDocumentPersons(ctx, tx, d.ID, d.PersonIDs)
	})
}

func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM documents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) GetDocument(ctx context.Context, id string) (*model.Document, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+documentCols+` FROM documents WHERE id = ?`, id)
	d, err := scanDocument(row)
	if err != nil {
		return nil, err
	}
	if d.PersonIDs, err = s.documentPersons(ctx, d.ID); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) ListDocuments(ctx context.Context) ([]model.Document, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+documentCols+` FROM documents ORDER BY title, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].PersonIDs, err = s.documentPersons(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) documentPersons(ctx context.Context, docID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT person_id FROM document_persons WHERE document_id = ? ORDER BY person_id`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func syncDocumentPersons(ctx context.Context, tx *sql.Tx, docID string, personIDs []string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM document_persons WHERE document_id = ?`, docID); err != nil {
		return err
	}
	for _, pid := range personIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO document_persons (document_id, person_id) VALUES (?, ?)`,
			docID, pid); err != nil {
			return err
		}
	}
	return nil
}

func scanDocument(row scanner) (*model.Document, error) {
	var d model.Document
	var kind string
	var body, fileID, reviewed sql.NullString
	var interval sql.NullFloat64
	var exportOK int
	if err := row.Scan(&d.ID, &d.Title, &kind, &body, &fileID, &interval, &reviewed, &exportOK); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Kind = model.DocumentKind(kind)
	d.Body = str(body)
	d.FileID = str(fileID)
	if interval.Valid {
		d.ReviewIntervalMonths = interval.Float64
	}
	d.LastReviewed = str(reviewed)
	d.ExportOK = exportOK != 0
	return &d, nil
}

// nzUnsetF maps 0 to NULL for fields where the schema's exclusiveMinimum
// makes 0 mean "not declared".
func nzUnsetF(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

// --- Maintenance events -------------------------------------------------

const maintCols = `id, date, kind, kit_id, item_id, lot_id, notes`

func validateMaintenance(m *model.MaintenanceEvent) error {
	if m.Date == "" {
		return vErrf("maintenance date is required")
	}
	if err := validDateField("date", m.Date); err != nil {
		return err
	}
	if !m.Kind.Valid() {
		return vErrf("invalid maintenance kind %q", m.Kind)
	}
	return nil
}

func (s *Store) CreateMaintenance(ctx context.Context, m *model.MaintenanceEvent) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	if err := validateMaintenance(m); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO maintenance_events (`+maintCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Date, string(m.Kind), nz(m.KitID), nz(m.ItemID), nz(m.LotID), nz(m.Notes))
	return err
}

func (s *Store) DeleteMaintenance(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM maintenance_events WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return mustAffect(res)
}

func (s *Store) ListMaintenance(ctx context.Context) ([]model.MaintenanceEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+maintCols+` FROM maintenance_events ORDER BY date DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.MaintenanceEvent
	for rows.Next() {
		var m model.MaintenanceEvent
		var kind string
		var kit, item, lot, notes sql.NullString
		if err := rows.Scan(&m.ID, &m.Date, &kind, &kit, &item, &lot, &notes); err != nil {
			return nil, err
		}
		m.Kind = model.MaintenanceKind(kind)
		m.KitID = str(kit)
		m.ItemID = str(item)
		m.LotID = str(lot)
		m.Notes = str(notes)
		out = append(out, m)
	}
	return out, rows.Err()
}
