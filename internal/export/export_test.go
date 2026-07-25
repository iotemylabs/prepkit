package export

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/internal/store"
	"github.com/iotemylabs/prepkit/schema"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

var ctx = context.Background()

func fp(v float64) *float64 { return &v }

// populated builds a store exercising every entity type, including all the
// sensitive fields and a document of a never-export kind.
func populated(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if err := s.UpdateHousehold(ctx, &model.Household{
		Label: "Home", Pets: 1, Climate: model.ClimateTemperate, Environment: model.EnvSuburban,
	}); err != nil {
		t.Fatal(err)
	}
	p := &model.Person{Label: "parent 1", AgeBand: model.AgeAdult, Needs: []string{"daily medication"}}
	if err := s.CreatePerson(ctx, p); err != nil {
		t.Fatal(err)
	}

	garage := &model.Container{Name: "Garage shelf", Type: model.ContShelf, LocationNote: "back wall, left of the water heater"}
	if err := s.CreateContainer(ctx, garage); err != nil {
		t.Fatal(err)
	}
	bin := &model.Container{Name: "Kit bin", Type: model.ContBin, ParentID: garage.ID, ColorCode: "blue"}
	if err := s.CreateContainer(ctx, bin); err != nil {
		t.Fatal(err)
	}

	kit := &model.Kit{Name: "Home 72h", TemplateID: "home-72hr-baseline", Layer: model.LayerHome,
		Duration: model.Duration72h, ContainerID: bin.ID, AssignedPersonID: p.ID, LastVerified: "2026-06-01"}
	if err := s.CreateKit(ctx, kit); err != nil {
		t.Fatal(err)
	}

	radio := &model.Item{Name: "Emergency radio", Category: model.CatComms, TemplateItemID: "emergency-radio",
		KitID: kit.ID, ContainerID: bin.ID, Quantity: fp(1), Unit: "ea",
		Product: "Midland ER310", Serial: "SN-99887", Cost: fp(59.99), Acquired: "2025-01-15"}
	if err := s.CreateItem(ctx, radio); err != nil {
		t.Fatal(err)
	}
	water := &model.Item{Name: "Stored water", Category: model.CatWater, TemplateItemID: "stored-drinking-water",
		KitID: kit.ID, Unit: "l"}
	if err := s.CreateItem(ctx, water); err != nil {
		t.Fatal(err)
	}
	for _, lot := range []*model.Lot{
		{ItemID: water.ID, Quantity: 8, Unit: "l", Expires: "2026-11-01", ExpiryBasis: model.ExpiryPrintedBestBy, NextCheck: "2026-09-01"},
		{ItemID: water.ID, Quantity: 4, Unit: "l", Expires: "2027-05-01", ExpiryBasis: model.ExpiryPrintedBestBy, Status: model.LotSealed},
	} {
		if err := s.CreateLot(ctx, lot); err != nil {
			t.Fatal(err)
		}
	}

	docs := []*model.Document{
		{Title: "Comms plan", Kind: model.DocPlan, Body: "meet at the library", ReviewIntervalMonths: 12,
			LastReviewed: "2026-01-05", ExportOK: true, PersonIDs: []string{p.ID}},
		{Title: "Neighborhood contacts", Kind: model.DocContactSheet, Body: "unit 4: the Larsens", ExportOK: false},
		{Title: "Passport scans", Kind: model.DocIdentity, Body: "secret", ExportOK: true},
	}
	for _, d := range docs {
		if err := s.CreateDocument(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.CreateMaintenance(ctx, &model.MaintenanceEvent{
		Date: "2026-06-01", Kind: model.MaintRepack, KitID: kit.ID, Notes: "annual repack",
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func compileInventorySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	const url = "https://prepkit.dev/schema/v0.1/inventory.schema.json"
	c := jsonschema.NewCompiler()
	if err := c.AddResource(url, bytes.NewReader(schema.Inventory)); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func TestExportValidatesAgainstSchema(t *testing.T) {
	s := populated(t)
	inv, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sch := compileInventorySchema(t)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)

	for _, opts := range []Options{{}, {IncludeSensitive: true}} {
		b, err := JSON(*inv, now, opts)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if err := sch.Validate(doc); err != nil {
			t.Errorf("export (opts %+v) does not conform to inventory.schema.json:\n%v", opts, err)
		}
	}
}

// TestRoundTrip: export everything exportable, import into a fresh database,
// export again, and confirm the result is identical. instance_id is
// per-installation and legitimately differs; never-export documents
// legitimately cannot survive any export, which is asserted separately.
func TestRoundTrip(t *testing.T) {
	src := populated(t)
	srcInv, err := src.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	exported := Filter(*srcInv, Options{IncludeSensitive: true})

	dst, err := store.Open(filepath.Join(t.TempDir(), "restore.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	if err := Import(ctx, dst, &exported); err != nil {
		t.Fatalf("import: %v", err)
	}

	dstInv, err := dst.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reexported := Filter(*dstInv, Options{IncludeSensitive: true})

	exported.InstanceID = ""
	reexported.InstanceID = ""
	if !reflect.DeepEqual(exported, reexported) {
		a, _ := json.MarshalIndent(exported, "", "  ")
		b, _ := json.MarshalIndent(reexported, "", "  ")
		t.Errorf("round trip not identical:\n--- exported ---\n%s\n--- reimported ---\n%s", a, b)
	}

	// The identity document must not have survived: never-export kinds
	// cannot leave the source instance through any export path.
	for _, d := range reexported.Documents {
		if d.Kind.NeverExportUnencrypted() {
			t.Errorf("never-export document %q crossed an export boundary", d.Title)
		}
	}
	if len(reexported.Documents) != 2 {
		t.Errorf("re-export has %d documents, want 2 (plan + contact sheet)", len(reexported.Documents))
	}
}

func TestSensitivityPolicy(t *testing.T) {
	s := populated(t)
	inv, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	def := Filter(*inv, Options{})
	for _, p := range def.Household.Persons {
		if p.Needs != nil {
			t.Error("person.needs must be stripped by default")
		}
	}
	for _, it := range def.Items {
		if it.Serial != "" || it.Cost != nil {
			t.Errorf("item %q: serial/cost must be stripped by default", it.Name)
		}
	}
	for _, c := range def.Containers {
		if c.LocationNote != "" {
			t.Errorf("container %q: location_note must be stripped by default", c.Name)
		}
	}
	if len(def.Documents) != 1 || def.Documents[0].Kind != model.DocPlan {
		t.Errorf("default export documents = %+v, want only the export_ok plan", def.Documents)
	}

	full := Filter(*inv, Options{IncludeSensitive: true})
	if full.Household.Persons[0].Needs == nil {
		t.Error("IncludeSensitive must restore needs")
	}
	foundSerial := false
	for _, it := range full.Items {
		if it.Serial == "SN-99887" {
			foundSerial = true
		}
	}
	if !foundSerial {
		t.Error("IncludeSensitive must restore serial")
	}
	// export_ok=false documents come along for backup, but never-export
	// kinds do not, even here.
	if len(full.Documents) != 2 {
		t.Errorf("full export documents = %d, want 2", len(full.Documents))
	}
	for _, d := range full.Documents {
		if d.Kind.NeverExportUnencrypted() {
			t.Errorf("never-export document %q included despite IncludeSensitive", d.Title)
		}
	}

	// The source snapshot itself is untouched by filtering.
	if inv.Items[0].Serial == "" && inv.Items[1].Serial == "" {
		t.Error("Filter must copy, not mutate the snapshot")
	}
}

func TestImportRefusals(t *testing.T) {
	s := populated(t)
	inv, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Non-empty target.
	if err := Import(ctx, s, inv); err == nil || !strings.Contains(err.Error(), "empty database") {
		t.Errorf("import into populated store: got %v, want empty-database refusal", err)
	}

	// Wrong spec version.
	fresh, err := store.Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	bad := *inv
	bad.SpecVersion = "9.9"
	if err := Import(ctx, fresh, &bad); err == nil || !strings.Contains(err.Error(), "spec_version") {
		t.Errorf("wrong spec version: got %v, want refusal", err)
	}
}
