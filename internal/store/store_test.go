package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/iotemylabs/prepkit/internal/model"
)

var ctx = context.Background()

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func fp(v float64) *float64 { return &v }

func tableNames(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[n] = true
	}
	return out
}

// TestMigrationsFreshDatabase runs every migration against an empty database.
func TestMigrationsFreshDatabase(t *testing.T) {
	s := newStore(t)
	tables := tableNames(t, s)
	for _, want := range []string{
		"instance", "household", "persons", "containers", "kits", "items",
		"lots", "documents", "document_persons", "maintenance_events",
		"schema_migrations",
	} {
		if !tables[want] {
			t.Errorf("table %s missing after migration", want)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("schema_migrations has %d rows, want 2", n)
	}
	id, err := s.InstanceID(ctx)
	if err != nil || id == "" {
		t.Errorf("instance id not initialized: %q, %v", id, err)
	}
}

// TestMigrationsOverPopulatedDatabase applies migration 001 only, populates
// it, then runs the remaining migrations over the live data — the upgrade
// path an existing installation takes.
func TestMigrationsOverPopulatedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	s, err := open(path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if tables := tableNames(t, s); tables["documents"] {
		t.Fatal("documents table exists at version 1; test premise broken")
	}
	kit := &model.Kit{Name: "Go Bag", Layer: model.LayerEvacuation}
	if err := s.CreateKit(ctx, kit); err != nil {
		t.Fatal(err)
	}
	item := &model.Item{Name: "Water", Category: model.CatWater, KitID: kit.ID}
	if err := s.CreateItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLot(ctx, &model.Lot{ItemID: item.ID, Quantity: 6, Expires: "2027-03-01"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopen with all migrations: 002 must apply over the populated 001.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("migrating populated database: %v", err)
	}
	defer s2.Close()
	if tables := tableNames(t, s2); !tables["documents"] {
		t.Error("migration 002 did not apply over populated database")
	}
	got, err := s2.GetKit(ctx, kit.ID)
	if err != nil || got.Name != "Go Bag" {
		t.Errorf("data lost across migration: %+v, %v", got, err)
	}
	lots, err := s2.ListLotsByItem(ctx, item.ID)
	if err != nil || len(lots) != 1 || lots[0].Quantity != 6 {
		t.Errorf("lots lost across migration: %+v, %v", lots, err)
	}

	// A third open is a no-op re-run.
	s2.Close()
	s3, err := Open(path)
	if err != nil {
		t.Fatalf("re-running migrations must be a no-op: %v", err)
	}
	s3.Close()
}

func TestContainerCycleRejected(t *testing.T) {
	s := newStore(t)

	a := &model.Container{Name: "Vehicle", Type: model.ContVehicle}
	if err := s.CreateContainer(ctx, a); err != nil {
		t.Fatal(err)
	}
	b := &model.Container{Name: "Pack", Type: model.ContBackpack, ParentID: a.ID}
	if err := s.CreateContainer(ctx, b); err != nil {
		t.Fatal(err)
	}
	c := &model.Container{Name: "Pouch", Type: model.ContPouch, ParentID: b.ID}
	if err := s.CreateContainer(ctx, c); err != nil {
		t.Fatal(err)
	}

	// Self-parentage is the depth-0 cycle.
	self := &model.Container{ID: NewID(), Name: "Ouroboros"}
	self.ParentID = self.ID
	if err := s.CreateContainer(ctx, self); !errors.Is(err, ErrContainerCycle) {
		t.Errorf("self-parent create: got %v, want ErrContainerCycle", err)
	}

	// Closing the loop vehicle -> pack -> pouch -> vehicle.
	a.ParentID = c.ID
	if err := s.UpdateContainer(ctx, a); !errors.Is(err, ErrContainerCycle) {
		t.Errorf("cycle update: got %v, want ErrContainerCycle", err)
	}

	// A nonexistent parent is a validation error, not a crash.
	d := &model.Container{Name: "Orphan", ParentID: "no-such-id"}
	if err := s.CreateContainer(ctx, d); !errors.Is(err, ErrValidation) {
		t.Errorf("missing parent: got %v, want ErrValidation", err)
	}
}

func TestItemQuantityInvariant(t *testing.T) {
	s := newStore(t)
	it := &model.Item{Name: "Canned chili", Category: model.CatFood, Quantity: fp(10), Unit: "ea"}
	if err := s.CreateItem(ctx, it); err != nil {
		t.Fatal(err)
	}

	// Adding the first lot must clear the item's own quantity.
	lot := &model.Lot{ItemID: it.ID, Quantity: 6, Expires: "2027-03-01", ExpiryBasis: model.ExpiryPrintedBestBy}
	if err := s.CreateLot(ctx, lot); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetItem(ctx, it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Quantity != nil {
		t.Errorf("item quantity = %v after first lot, want nil (derived from lots)", *got.Quantity)
	}

	// Setting a quantity while lots exist is rejected.
	got.Quantity = fp(4)
	if err := s.UpdateItem(ctx, got); !errors.Is(err, ErrHasLots) {
		t.Errorf("quantity write with lots: got %v, want ErrHasLots", err)
	}

	// After the last lot is gone the user may set quantity again.
	if err := s.DeleteLot(ctx, lot.ID); err != nil {
		t.Fatal(err)
	}
	got.Quantity = fp(4)
	if err := s.UpdateItem(ctx, got); err != nil {
		t.Errorf("quantity write with no lots: %v", err)
	}
}

func TestKitDeleteOrphansItems(t *testing.T) {
	s := newStore(t)
	kit := &model.Kit{Name: "Home Kit", Layer: model.LayerHome}
	if err := s.CreateKit(ctx, kit); err != nil {
		t.Fatal(err)
	}
	it := &model.Item{Name: "Radio", Category: model.CatComms, KitID: kit.ID, Quantity: fp(1)}
	if err := s.CreateItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteKit(ctx, kit.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetItem(ctx, it.ID)
	if err != nil {
		t.Fatalf("item destroyed with kit: %v", err)
	}
	if got.KitID != "" {
		t.Errorf("item kit_id = %q after kit delete, want empty", got.KitID)
	}
}

func TestContainerDeleteRefusedWhenInUse(t *testing.T) {
	s := newStore(t)
	parent := &model.Container{Name: "Shelf"}
	if err := s.CreateContainer(ctx, parent); err != nil {
		t.Fatal(err)
	}
	child := &model.Container{Name: "Bin", ParentID: parent.ID}
	if err := s.CreateContainer(ctx, child); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteContainer(ctx, parent.ID); !errors.Is(err, ErrInUse) {
		t.Errorf("delete with child: got %v, want ErrInUse", err)
	}

	it := &model.Item{Name: "Rope", Category: model.CatTools, ContainerID: child.ID, Quantity: fp(1)}
	if err := s.CreateItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteContainer(ctx, child.ID); !errors.Is(err, ErrInUse) {
		t.Errorf("delete with contents: got %v, want ErrInUse", err)
	}

	// Empty it and deletion proceeds.
	if err := s.DeleteItem(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteContainer(ctx, child.ID); err != nil {
		t.Errorf("delete of empty container: %v", err)
	}
	if err := s.DeleteContainer(ctx, parent.ID); err != nil {
		t.Errorf("delete of now-childless container: %v", err)
	}
}

func TestItemDeleteCascadesLots(t *testing.T) {
	s := newStore(t)
	it := &model.Item{Name: "Rice", Category: model.CatFood}
	if err := s.CreateItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	for _, exp := range []string{"2027-01-01", "2029-06-01"} {
		if err := s.CreateLot(ctx, &model.Lot{ItemID: it.ID, Quantity: 2, Expires: exp}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteItem(ctx, it.ID); err != nil {
		t.Fatal(err)
	}
	lots, err := s.ListLots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lots) != 0 {
		t.Errorf("%d orphan lots survived item delete", len(lots))
	}
}

func TestQRSlugGeneration(t *testing.T) {
	s := newStore(t)
	a := &model.Container{Name: "A"}
	b := &model.Container{Name: "B"}
	if err := s.CreateContainer(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateContainer(ctx, b); err != nil {
		t.Fatal(err)
	}
	form := regexp.MustCompile(`^PK-[0-9A-HJKMNP-TV-Z]{4}-[0-9A-HJKMNP-TV-Z]{4}$`)
	for _, c := range []*model.Container{a, b} {
		if !form.MatchString(c.QRSlug) {
			t.Errorf("slug %q not in expected opaque form", c.QRSlug)
		}
	}
	if a.QRSlug == b.QRSlug {
		t.Error("slugs must be unique")
	}
	got, err := s.GetContainerByQRSlug(ctx, a.QRSlug)
	if err != nil || got.ID != a.ID {
		t.Errorf("slug resolution failed: %+v, %v", got, err)
	}
}

func TestItemRoundTrip(t *testing.T) {
	s := newStore(t)
	cont := &model.Container{Name: "Bin"}
	if err := s.CreateContainer(ctx, cont); err != nil {
		t.Fatal(err)
	}
	kit := &model.Kit{Name: "Kit"}
	if err := s.CreateKit(ctx, kit); err != nil {
		t.Fatal(err)
	}
	want := &model.Item{
		Name:           "Water filter",
		Category:       model.CatWater,
		TemplateItemID: "water-filter",
		KitID:          kit.ID,
		ContainerID:    cont.ID,
		Quantity:       fp(1),
		Unit:           "ea",
		Product:        "Sawyer Squeeze",
		WeightG:        fp(85),
		Acquired:       "2025-11-02",
		Cost:           fp(39.95),
		PhotoIDs:       []string{NewID()},
		Serial:         "SN-123456",
		Notes:          "backflush every use",
		Archived:       false,
	}
	if err := s.CreateItem(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetItem(ctx, want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

func TestHouseholdUpsertAndValidation(t *testing.T) {
	s := newStore(t)

	// Never-configured household reads back empty.
	h, err := s.GetHousehold(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Label != "" || len(h.Persons) != 0 {
		t.Errorf("expected empty household, got %+v", h)
	}

	// "any" is a template assumption, not a household climate.
	if err := s.UpdateHousehold(ctx, &model.Household{Climate: model.ClimateAny}); !errors.Is(err, ErrValidation) {
		t.Errorf("climate any: got %v, want ErrValidation", err)
	}

	want := &model.Household{Label: "Home", Pets: 2, Climate: model.ClimateCold, Environment: model.EnvRural}
	if err := s.UpdateHousehold(ctx, want); err != nil {
		t.Fatal(err)
	}
	p := &model.Person{Label: "parent 1", AgeBand: model.AgeAdult, Needs: []string{"daily medication"}}
	if err := s.CreatePerson(ctx, p); err != nil {
		t.Fatal(err)
	}

	h, err = s.GetHousehold(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.Label != "Home" || h.Pets != 2 || h.Climate != model.ClimateCold {
		t.Errorf("household mismatch: %+v", h)
	}
	if len(h.Persons) != 1 || h.Persons[0].Needs[0] != "daily medication" {
		t.Errorf("persons mismatch: %+v", h.Persons)
	}

	// Upsert replaces in place.
	want.Pets = 0
	if err := s.UpdateHousehold(ctx, want); err != nil {
		t.Fatal(err)
	}
	h, _ = s.GetHousehold(ctx)
	if h.Pets != 0 {
		t.Errorf("upsert did not replace pets: %+v", h)
	}
}

func TestDocumentPersonSync(t *testing.T) {
	s := newStore(t)
	p1 := &model.Person{Label: "parent 1"}
	p2 := &model.Person{Label: "parent 2"}
	for _, p := range []*model.Person{p1, p2} {
		if err := s.CreatePerson(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	d := &model.Document{Title: "Comms plan", Kind: model.DocPlan, Body: "meet at the library",
		ReviewIntervalMonths: 12, PersonIDs: []string{p1.ID, p2.ID}}
	if err := s.CreateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDocument(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PersonIDs) != 2 {
		t.Errorf("person ids = %v, want both", got.PersonIDs)
	}
	if got.ExportOK {
		t.Error("export_ok must default to false")
	}

	// Update narrows the link set.
	d.PersonIDs = []string{p2.ID}
	if err := s.UpdateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDocument(ctx, d.ID)
	if len(got.PersonIDs) != 1 || got.PersonIDs[0] != p2.ID {
		t.Errorf("person ids after update = %v, want [%s]", got.PersonIDs, p2.ID)
	}

	// Deleting a person cleans the join table via cascade.
	if err := s.DeletePerson(ctx, p2.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDocument(ctx, d.ID)
	if len(got.PersonIDs) != 0 {
		t.Errorf("person ids after person delete = %v, want none", got.PersonIDs)
	}
}

func TestValidationErrors(t *testing.T) {
	s := newStore(t)
	cases := []struct {
		name string
		err  error
	}{
		{"item without name", s.CreateItem(ctx, &model.Item{Category: model.CatFood})},
		{"item bad category", s.CreateItem(ctx, &model.Item{Name: "x", Category: "nonsense"})},
		{"kit bad layer", s.CreateKit(ctx, &model.Kit{Name: "x", Layer: "orbit"})},
		{"kit bad date", s.CreateKit(ctx, &model.Kit{Name: "x", LastVerified: "last spring"})},
		{"lot bad status", s.CreateLot(ctx, &model.Lot{ItemID: "i", Quantity: 1, Status: "vaporized"})},
		{"lot bad date", s.CreateLot(ctx, &model.Lot{ItemID: "i", Quantity: 1, Expires: "2027-3-1"})},
		{"lot negative quantity", s.CreateLot(ctx, &model.Lot{ItemID: "i", Quantity: -1})},
		{"document bad kind", s.CreateDocument(ctx, &model.Document{Title: "x", Kind: "scroll"})},
		{"maintenance bad kind", s.CreateMaintenance(ctx, &model.MaintenanceEvent{Date: "2026-01-01", Kind: "vibes"})},
	}
	for _, c := range cases {
		if !errors.Is(c.err, ErrValidation) {
			t.Errorf("%s: got %v, want ErrValidation", c.name, c.err)
		}
	}

	// A lot pointing at a missing item is ErrNotFound, distinct from field
	// validation.
	if err := s.CreateLot(ctx, &model.Lot{ItemID: NewID(), Quantity: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("lot for missing item: got %v, want ErrNotFound", err)
	}
}

// TestBackup: the copy is complete, including a document kind no export may
// carry, it refuses to overwrite, and the copy opens as a normal database.
func TestBackup(t *testing.T) {
	s := newStore(t)
	it := &model.Item{Name: "Radio", Category: model.CatComms, Serial: "SN-42", Quantity: fp(1)}
	if err := s.CreateItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	doc := &model.Document{Title: "Passport scans", Kind: model.DocIdentity, Body: "secret"}
	if err := s.CreateDocument(ctx, doc); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "backup with space.db")
	if err := s.Backup(ctx, dest); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if err := s.Backup(ctx, dest); err == nil {
		t.Error("backup overwrote an existing file")
	}

	restored, err := Open(dest)
	if err != nil {
		t.Fatalf("opening backup: %v", err)
	}
	defer restored.Close()
	inv, err := restored.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Items) != 1 || inv.Items[0].Serial != "SN-42" {
		t.Errorf("backup items = %+v, want the radio with its serial", inv.Items)
	}
	if len(inv.Documents) != 1 || inv.Documents[0].Kind != model.DocIdentity {
		t.Errorf("backup documents = %+v, want the identity document", inv.Documents)
	}
}
