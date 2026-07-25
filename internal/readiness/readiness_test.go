package readiness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
)

// fixture is one table-driven test case: a full Input plus selective
// expectations. Fields the fixture omits are simply not asserted.
type fixture struct {
	Description string                   `json:"description"`
	Now         string                   `json:"now"`
	Config      *Config                  `json:"config"`
	Template    model.Template           `json:"template"`
	Household   *model.Household         `json:"household"`
	Kit         model.Kit                `json:"kit"`
	Items       []model.Item             `json:"items"`
	Lots        []model.Lot              `json:"lots"`
	Maintenance []model.MaintenanceEvent `json:"maintenance"`
	Expect      expect                   `json:"expect"`
}

type expect struct {
	Incomplete        *bool               `json:"incomplete"`
	Essential         *covExp             `json:"essential"`
	Recommended       *covExp             `json:"recommended"`
	Optional          *covExp             `json:"optional"`
	Lines             map[string]lineExp  `json:"lines"`
	Groups            map[string]groupExp `json:"groups"`
	ExpiringSoon      []string            `json:"expiring_soon"`       // exact lot id set
	Expired           []string            `json:"expired"`             // exact lot id set
	CheckOverdueItems []string            `json:"check_overdue_items"` // exact item id set
	Unverified        *bool               `json:"unverified"`
	WarningsContain   []string            `json:"warnings_contain"`
	Weight            *weightExp          `json:"weight"`
}

type covExp struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

type lineExp struct {
	Status          string   `json:"status"`
	Ratio           *float64 `json:"ratio"`
	ScaledTarget    *float64 `json:"scaled_target"`
	ValidQuantity   *float64 `json:"valid_quantity"`
	ExpiredQuantity *float64 `json:"expired_quantity"`
	CoveredBy       string   `json:"covered_by"`
	WarningsContain []string `json:"warnings_contain"`
}

type groupExp struct {
	Covered   bool `json:"covered"`
	Redundant bool `json:"redundant"`
}

type weightExp struct {
	EstimateKg  *float64 `json:"estimate_kg"`
	OverCeiling *bool    `json:"over_ceiling"`
	IsFloor     *bool    `json:"is_floor"`
}

func TestFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures found")
	}
	for _, file := range files {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(strings.NewReader(string(b)))
			dec.DisallowUnknownFields()
			var fx fixture
			if err := dec.Decode(&fx); err != nil {
				t.Fatalf("decoding fixture: %v", err)
			}
			now, err := model.ParseDate(fx.Now)
			if err != nil {
				t.Fatalf("fixture now: %v", err)
			}
			cfg := Config{}
			if fx.Config != nil {
				cfg = *fx.Config
			}
			r := Score(Input{
				Template:  fx.Template,
				Items:     fx.Template.Items,
				Household: fx.Household,
				Kit:       fx.Kit,
				UserItems: fx.Items,
				Lots:      fx.Lots,
				Events:    fx.Maintenance,
				Now:       now,
			}, cfg)
			assertReport(t, fx.Expect, r)
		})
	}
}

func assertReport(t *testing.T, e expect, r *Report) {
	t.Helper()
	if e.Incomplete != nil && r.Incomplete != *e.Incomplete {
		t.Errorf("incomplete = %v, want %v", r.Incomplete, *e.Incomplete)
	}
	checkCov := func(name string, got Coverage, want *covExp) {
		if want == nil {
			return
		}
		if got.Covered != want.Covered || got.Total != want.Total {
			t.Errorf("%s = %d/%d, want %d/%d", name, got.Covered, got.Total, want.Covered, want.Total)
		}
	}
	checkCov("essential", r.Essential, e.Essential)
	checkCov("recommended", r.Recommended, e.Recommended)
	checkCov("optional", r.Optional, e.Optional)

	lineByID := map[string]Line{}
	for _, l := range r.Lines {
		lineByID[l.TemplateItemID] = l
	}
	for id, want := range e.Lines {
		got, ok := lineByID[id]
		if !ok {
			t.Errorf("line %s missing from report", id)
			continue
		}
		if want.Status != "" && string(got.Status) != want.Status {
			t.Errorf("line %s status = %s, want %s", id, got.Status, want.Status)
		}
		if want.Ratio != nil && !close(got.Ratio, *want.Ratio) {
			t.Errorf("line %s ratio = %g, want %g", id, got.Ratio, *want.Ratio)
		}
		if want.ScaledTarget != nil && !close(got.ScaledTarget, *want.ScaledTarget) {
			t.Errorf("line %s scaled target = %g, want %g", id, got.ScaledTarget, *want.ScaledTarget)
		}
		if want.ValidQuantity != nil && !close(got.ValidQuantity, *want.ValidQuantity) {
			t.Errorf("line %s valid quantity = %g, want %g", id, got.ValidQuantity, *want.ValidQuantity)
		}
		if want.ExpiredQuantity != nil && !close(got.ExpiredQuantity, *want.ExpiredQuantity) {
			t.Errorf("line %s expired quantity = %g, want %g", id, got.ExpiredQuantity, *want.ExpiredQuantity)
		}
		if want.CoveredBy != "" && got.CoveredBy != want.CoveredBy {
			t.Errorf("line %s covered_by = %q, want %q", id, got.CoveredBy, want.CoveredBy)
		}
		for _, sub := range want.WarningsContain {
			if !anyContains(got.Warnings, sub) {
				t.Errorf("line %s warnings %v do not mention %q", id, got.Warnings, sub)
			}
		}
	}

	groupByName := map[string]GroupReport{}
	for _, g := range r.Groups {
		groupByName[g.Name] = g
	}
	for name, want := range e.Groups {
		got, ok := groupByName[name]
		if !ok {
			t.Errorf("group %s missing from report", name)
			continue
		}
		if got.Covered != want.Covered || got.Redundant != want.Redundant {
			t.Errorf("group %s = covered:%v redundant:%v, want covered:%v redundant:%v",
				name, got.Covered, got.Redundant, want.Covered, want.Redundant)
		}
	}

	if e.ExpiringSoon != nil {
		var got []string
		for _, f := range r.Freshness.ExpiringSoon {
			got = append(got, f.LotID)
		}
		assertSet(t, "expiring_soon", got, e.ExpiringSoon)
	}
	if e.Expired != nil {
		var got []string
		for _, f := range r.Freshness.Expired {
			got = append(got, f.LotID)
		}
		assertSet(t, "expired", got, e.Expired)
	}
	if e.CheckOverdueItems != nil {
		var got []string
		for _, f := range r.Freshness.CheckOverdue {
			got = append(got, f.ItemID)
		}
		assertSet(t, "check_overdue_items", got, e.CheckOverdueItems)
	}
	if e.Unverified != nil && r.Freshness.Unverified != *e.Unverified {
		t.Errorf("unverified = %v (%s), want %v", r.Freshness.Unverified, r.Freshness.UnverifiedReason, *e.Unverified)
	}
	for _, sub := range e.WarningsContain {
		if !anyContains(r.Warnings, sub) {
			t.Errorf("report warnings %v do not mention %q", r.Warnings, sub)
		}
	}
	if e.Weight != nil {
		if r.Weight == nil {
			t.Error("weight report missing")
		} else {
			if e.Weight.EstimateKg != nil && !close(r.Weight.EstimateKg, *e.Weight.EstimateKg) {
				t.Errorf("weight estimate = %g, want %g", r.Weight.EstimateKg, *e.Weight.EstimateKg)
			}
			if e.Weight.OverCeiling != nil && r.Weight.OverCeiling != *e.Weight.OverCeiling {
				t.Errorf("over ceiling = %v, want %v", r.Weight.OverCeiling, *e.Weight.OverCeiling)
			}
			if e.Weight.IsFloor != nil && r.Weight.IsFloor != *e.Weight.IsFloor {
				t.Errorf("is floor = %v, want %v", r.Weight.IsFloor, *e.Weight.IsFloor)
			}
		}
	}
}

func close(a, b float64) bool {
	d := a - b
	return d < 1e-9 && d > -1e-9
}

func anyContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func assertSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if strings.Join(g, ",") != strings.Join(w, ",") {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

// --- unit tests for the helpers ----------------------------------------

func TestScaledTargetTable(t *testing.T) {
	item := func(q float64, s model.Scaling) model.TemplateItem {
		return model.TemplateItem{Quantity: q, Scaling: s}
	}
	cases := []struct {
		name     string
		it       model.TemplateItem
		persons  int
		pets     int
		duration model.Duration
		want     float64
		scored   bool
	}{
		{"fixed", item(2, model.ScaleFixed), 4, 2, model.Duration72h, 2, true},
		{"default is fixed", item(2, ""), 4, 2, model.Duration72h, 2, true},
		{"per_person", item(1.5, model.ScalePerPerson), 3, 0, model.Duration72h, 4.5, true},
		{"per_person_per_day 72h", item(4, model.ScalePerPersonPerDay), 2, 0, model.Duration72h, 24, true},
		{"per_person_per_day 1y", item(1, model.ScalePerPersonPerDay), 1, 0, model.Duration1y, 365, true},
		{"per_pet", item(3, model.ScalePerPet), 1, 2, model.Duration72h, 6, true},
		{"per_pet none", item(3, model.ScalePerPet), 1, 0, model.Duration72h, 0, true},
		{"per_vehicle is 1", item(2, model.ScalePerVehicle), 5, 0, model.Duration72h, 2, true},
		{"per_household", item(2, model.ScalePerHousehold), 5, 0, model.Duration72h, 2, true},
		{"indefinite unscorable", item(4, model.ScalePerPersonPerDay), 2, 0, model.DurationIndefinite, 0, false},
		{"no duration unscorable", item(4, model.ScalePerPersonPerDay), 2, 0, "", 0, false},
	}
	for _, c := range cases {
		got, scored, _ := scaledTarget(c.it, c.persons, c.pets, c.duration)
		if scored != c.scored || (scored && !close(got, c.want)) {
			t.Errorf("%s: got (%g, %v), want (%g, %v)", c.name, got, scored, c.want, c.scored)
		}
	}
}

func TestAddMonths(t *testing.T) {
	base := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if got := addMonths(base, 6); !got.Equal(time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("whole months: got %v", got)
	}
	// Half a month is ~15.22 days.
	got := addMonths(base, 0.5)
	if got.Before(base.AddDate(0, 0, 15)) || got.After(base.AddDate(0, 0, 16)) {
		t.Errorf("fractional month: got %v", got)
	}
}

func TestCoverageFraction(t *testing.T) {
	if _, ok := (Coverage{}).Fraction(); ok {
		t.Error("empty level must report no fraction, not 0% or 100%")
	}
	f, ok := (Coverage{Covered: 3, Total: 4}).Fraction()
	if !ok || !close(f, 0.75) {
		t.Errorf("got (%g, %v)", f, ok)
	}
}
