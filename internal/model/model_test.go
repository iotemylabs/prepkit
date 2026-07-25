package model

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCategoryColorMapping(t *testing.T) {
	// The eleven coloured categories per docs/categories.md.
	want := map[Category]string{
		CatWater:    "blue",
		CatFood:     "green",
		CatShelter:  "brown",
		CatFirstAid: "red",
		CatComms:    "yellow",
		CatFire:     "orange",
		CatTools:    "black",
		CatHygiene:  "white",
		CatClothing: "purple",
		CatPersonal: "pink",
		CatCooking:  "gray",
	}
	neutral := []Category{CatPower, CatDocuments, CatNavigation, CatSecurity}

	if len(want)+len(neutral) != len(Categories) {
		t.Fatalf("test covers %d categories, model declares %d", len(want)+len(neutral), len(Categories))
	}
	for cat, color := range want {
		got, ok := cat.Color()
		if !ok || got != color {
			t.Errorf("%s: got (%q, %v), want (%q, true)", cat, got, ok, color)
		}
	}
	for _, cat := range neutral {
		if got, ok := cat.Color(); ok {
			t.Errorf("%s: got colour %q, want none (rendered neutrally)", cat, got)
		}
	}
}

func TestDurationDays(t *testing.T) {
	// The mapping defined in docs/readiness.md.
	want := map[Duration]float64{
		Duration72h: 3, Duration2w: 14, Duration1m: 30, Duration3m: 90, Duration1y: 365,
	}
	for d, days := range want {
		got, ok := d.Days()
		if !ok || got != days {
			t.Errorf("%s: got (%v, %v), want (%v, true)", d, got, ok, days)
		}
	}
	if _, ok := DurationIndefinite.Days(); ok {
		t.Error("indefinite must have no defined day count; those lines are reported unscored")
	}
}

func TestLotStatusCountsTowardStock(t *testing.T) {
	for _, s := range []LotStatus{LotSealed, LotOpened, ""} {
		if !s.CountsTowardStock() {
			t.Errorf("status %q should count toward stock", s)
		}
	}
	for _, s := range []LotStatus{LotConsumed, LotDiscarded, LotRotatedOut} {
		if s.CountsTowardStock() {
			t.Errorf("status %q must never count toward stock", s)
		}
	}
}

func TestNeverExportUnencrypted(t *testing.T) {
	blocked := []DocumentKind{DocIdentity, DocFinancial, DocInsurance, DocMedical}
	for _, k := range blocked {
		if !k.NeverExportUnencrypted() {
			t.Errorf("%s must be barred from unencrypted export unconditionally", k)
		}
	}
	for _, k := range []DocumentKind{DocPlan, DocContactSheet, DocProperty, DocManual, DocReference, DocOther} {
		if k.NeverExportUnencrypted() {
			t.Errorf("%s should be exportable subject to its own export_ok", k)
		}
	}
}

func TestItemQuantityNullability(t *testing.T) {
	// An item with lots has nil quantity, and the field must be absent from
	// JSON entirely, per the schema's "MUST be null or absent when the item
	// has lots".
	b, err := json.Marshal(Item{ID: "x", Name: "chili", Category: CatFood})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "quantity") {
		t.Errorf("nil quantity must be omitted, got %s", b)
	}

	// A recorded quantity of 0 is a legal value and must survive.
	zero := 0.0
	b, err = json.Marshal(Item{ID: "x", Name: "chili", Category: CatFood, Quantity: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"quantity":0`) {
		t.Errorf("explicit zero quantity must serialize, got %s", b)
	}
}

func TestDocumentExportOKAlwaysExplicit(t *testing.T) {
	b, err := json.Marshal(Document{ID: "d", Title: "t", Kind: DocPlan})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"export_ok":false`) {
		t.Errorf("export_ok must serialize explicitly even when false, got %s", b)
	}
}

func TestParseDateStrict(t *testing.T) {
	if _, err := ParseDate("2027-03-15"); err != nil {
		t.Errorf("valid date rejected: %v", err)
	}
	for _, bad := range []string{"2027-3-15", "15/03/2027", "2027-13-01", "2027-03-15T00:00:00Z", ""} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) should fail", bad)
		}
	}
}

// TestTemplateDecodesVendoredPack decodes a real bundled template with
// KnownFields enabled, so any struct tag that drifts from the schema's field
// names fails loudly here rather than silently dropping data.
func TestTemplateDecodesVendoredPack(t *testing.T) {
	f, err := os.Open("../../templates/get-home-bag-urban.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var tpl Template
	if err := dec.Decode(&tpl); err != nil {
		t.Fatalf("decoding vendored template: %v", err)
	}

	if tpl.SpecVersion != SpecVersion {
		t.Errorf("spec_version = %q, want %q", tpl.SpecVersion, SpecVersion)
	}
	if tpl.ID != "get-home-bag-urban" || tpl.Layer != LayerOffsite || tpl.Duration != Duration72h {
		t.Errorf("header fields wrong: %+v", tpl)
	}
	if len(tpl.Items) != 28 {
		t.Errorf("got %d items, want 28", len(tpl.Items))
	}
	if tpl.Container == nil || tpl.Container.TargetWeightKg != 6 {
		t.Error("container.target_weight_kg not decoded")
	}
	if tpl.Source == nil || tpl.Source.License != "CC0-1.0" {
		t.Error("source.license not decoded")
	}

	byID := map[string]TemplateItem{}
	for _, it := range tpl.Items {
		byID[it.ID] = it
	}
	water := byID["water-carried"]
	if water.Quantity != 1.5 || water.Unit != UnitL || water.EffectiveCriticality() != CritEssential {
		t.Errorf("water-carried decoded wrong: %+v", water)
	}
	if water.Consumable == nil || water.Consumable.ShelfLifeMonths != 12 || water.Consumable.RotationPolicy != RotateFIFO {
		t.Errorf("water-carried consumable decoded wrong: %+v", water.Consumable)
	}
	if water.WeightG == nil || *water.WeightG != 1500 {
		t.Error("water-carried weight_g not decoded")
	}
	if byID["flashlight"].RedundancyGroup != "lighting" {
		t.Error("redundancy_group not decoded")
	}
	pb := byID["power-bank"]
	if pb.Consumable == nil || !pb.Consumable.DegradesWithoutDate {
		t.Error("degrades_without_expiry not decoded")
	}
	if byID["multi-tool"].RegulatoryNote == "" {
		t.Error("regulatory_note not decoded")
	}
	// Defaults: duct-tape declares optional; spare-socks declares recommended
	// explicitly; an item with no criticality must default to recommended.
	if byID["duct-tape"].EffectiveCriticality() != CritOptional {
		t.Error("explicit criticality not decoded")
	}
	if (TemplateItem{}).EffectiveCriticality() != CritRecommended {
		t.Error("criticality default must be recommended")
	}
	if (TemplateItem{}).EffectiveScaling() != ScaleFixed {
		t.Error("scaling default must be fixed")
	}
}
