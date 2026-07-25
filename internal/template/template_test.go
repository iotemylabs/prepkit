package template

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/templates"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestBundledTemplatesValid(t *testing.T) {
	p, r := Load(templates.FS)
	Validate(p, r)

	for _, e := range r.Errors {
		t.Errorf("unexpected error: %s", e)
	}
	for _, w := range r.Warnings {
		t.Errorf("unexpected warning: %s", w)
	}
	if len(p.IDs) != 3 {
		t.Fatalf("loaded %d templates, want 3", len(p.IDs))
	}

	byID := map[string]Summary{}
	for _, s := range r.Summaries {
		byID[s.ID] = s
	}

	// The integration fixture from the build brief: vehicle-kit-baseline
	// extends get-home-bag-urban and resolves 21 own lines to 41. Any other
	// count means the resolver is wrong.
	v := byID["vehicle-kit-baseline"]
	if v.OwnLines != 21 || v.Resolved != 41 {
		t.Errorf("vehicle-kit-baseline: %d own -> %d resolved, want 21 -> 41", v.OwnLines, v.Resolved)
	}
	if g := byID["get-home-bag-urban"]; g.Resolved != 28 {
		t.Errorf("get-home-bag-urban: %d resolved, want 28", g.Resolved)
	}
	if h := byID["home-72hr-baseline"]; h.Resolved != 28 {
		t.Errorf("home-72hr-baseline: %d resolved, want 28", h.Resolved)
	}

	// The get-home bag declares a 6 kg ceiling and full weights; the
	// estimate must come in under it with nothing unknown.
	g := byID["get-home-bag-urban"]
	if !g.HasCeiling || g.CeilingKg != 6 {
		t.Fatalf("get-home-bag-urban ceiling not detected: %+v", g)
	}
	if g.EstimateKg <= 0 || g.EstimateKg > 6 {
		t.Errorf("estimate %.2f kg out of expected range", g.EstimateKg)
	}
	if len(g.UnknownWeightIDs) != 0 {
		t.Errorf("unexpected unknown weights: %v", g.UnknownWeightIDs)
	}
}

// goldenLine is the projection of a resolved item written to the golden
// file: enough to catch any merge, override, removal, or ordering error.
type goldenLine struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Category model.Category    `json:"category"`
	Quantity float64           `json:"quantity"`
	Unit     model.Unit        `json:"unit"`
	Scaling  model.Scaling     `json:"scaling"`
	Crit     model.Criticality `json:"criticality"`
}

func TestVehicleResolutionGolden(t *testing.T) {
	p, r := Load(templates.FS)
	Validate(p, r)
	if !r.OK() {
		t.Fatalf("bundled templates invalid: %v", r.Errors)
	}

	var got []goldenLine
	for _, it := range p.ByID["vehicle-kit-baseline"].Resolved {
		got = append(got, goldenLine{
			ID:       it.ID,
			Name:     it.Name,
			Category: it.Category,
			Quantity: it.Quantity,
			Unit:     it.Unit,
			Scaling:  it.EffectiveScaling(),
			Crit:     it.EffectiveCriticality(),
		})
	}
	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	gotJSON = append(gotJSON, '\n')

	golden := "testdata/vehicle-kit-resolved.golden.json"
	if *update {
		if err := os.WriteFile(golden, gotJSON, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create): %v", err)
	}
	if string(want) != string(gotJSON) {
		t.Errorf("resolved vehicle kit differs from golden file %s\ngot:\n%s", golden, gotJSON)
	}

	// Spot-check the three merge behaviours independently of the golden
	// file, so a bad -update can't silently bless a regression.
	byID := map[string]goldenLine{}
	for _, l := range got {
		byID[l.ID] = l
	}
	if _, present := byID["paper-map"]; present {
		t.Error("paper-map was removed by quantity 0 and must not survive resolution")
	}
	if w := byID["water-carried"]; w.Quantity != 4 || w.Scaling != model.ScalePerPerson || w.Name != "Stored water in vehicle" {
		t.Errorf("water-carried override not applied: %+v", w)
	}
	if _, present := byID["regional-road-map"]; !present {
		t.Error("child addition regional-road-map missing")
	}
	if byID["walking-shoes"].Quantity != 1 {
		t.Error("inherited walking-shoes line lost")
	}
	// Order: inherited lines first (parent order), additions appended.
	if got[0].ID != "water-carried" {
		t.Errorf("first resolved line = %s, want water-carried (parent order preserved)", got[0].ID)
	}
	if got[len(got)-1].ID != "vehicle-documents" {
		t.Errorf("last resolved line = %s, want vehicle-documents (additions appended in child order)", got[len(got)-1].ID)
	}
}

// TestNegativeSixErrors ports the spec repo's CI negative test: the
// deliberately broken template contains six distinct errors and the Go
// validator must catch every one of them. The Go validator additionally
// enforces the license rule, so the missing source block is a seventh.
func TestNegativeSixErrors(t *testing.T) {
	p, r := Load(os.DirFS("testdata/negative"))
	Validate(p, r)

	if r.OK() {
		t.Fatal("validator accepted a template with six known errors")
	}
	var msgs []string
	for _, e := range r.Errors {
		msgs = append(msgs, e.Msg)
	}
	all := strings.Join(msgs, "\n")

	assertHas := func(desc, substr string) {
		t.Helper()
		if !strings.Contains(all, substr) {
			t.Errorf("missing %s: no error contains %q\ngot:\n%s", desc, substr, all)
		}
	}
	// 1. Schema: edc_tier is not allowed when layer is not "self" (the
	// allOf/else/not constraint; jsonschema/v5 phrases the leaf as "not
	// failed" at the document root — the readable duplicate is error 3).
	assertHas("schema edc_tier conditional", "(root): not failed")
	// 2. Schema: category "nonsense" is not in the enum.
	assertHas("schema category enum", "items/3/category")
	// 3. Cross-field: readable edc_tier/layer message.
	assertHas("cross-field edc_tier check", `edc_tier set but layer is "home"`)
	// 4. Cross-field: substitute referencing a nonexistent item.
	assertHas("orphan substitute", `lists substitute "does-not-exist" which does not exist`)
	// 5. Cross-field: quantity 0 in a base template.
	assertHas("zero quantity in base", `"zero-in-base" has quantity 0 in a base template`)
	// 6. Cross-field: degrades_without_expiry with no check interval.
	assertHas("degrades without check interval", `"no-check" degrades_without_expiry but has no check_interval_months`)
	// 7. (Go-only) License rule from the build brief.
	assertHas("license refusal", "no source.license declared")
}

// minimal returns a schema-valid single-item template for building edge-case
// fixtures in memory.
func minimal(id, extends, extraItems string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "spec_version: \"0.1\"\nid: %s\nname: T %s\nlayer: home\n", id, id)
	if extends != "" {
		fmt.Fprintf(&b, "extends: %s\n", extends)
	}
	b.WriteString("items:\n  - id: base-item\n    name: Base Item\n    category: water\n    quantity: 1\n    unit: ea\n")
	b.WriteString(extraItems)
	b.WriteString("source:\n  author: test\n  license: CC0-1.0\n")
	return b.String()
}

func fsOf(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func errsContain(r *Report, substr string) bool {
	for _, e := range r.Errors {
		if strings.Contains(e.Msg, substr) {
			return true
		}
	}
	return false
}

func warnsContain(r *Report, substr string) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w.Msg, substr) {
			return true
		}
	}
	return false
}

func TestExtendsDepthLimit(t *testing.T) {
	// Four extends edges: a -> b -> c -> d -> e. Three is the maximum.
	p, r := Load(fsOf(map[string]string{
		"a.yaml": minimal("a", "b", ""),
		"b.yaml": minimal("b", "c", ""),
		"c.yaml": minimal("c", "d", ""),
		"d.yaml": minimal("d", "e", ""),
		"e.yaml": minimal("e", "", ""),
	}))
	Validate(p, r)
	if !errsContain(r, "extends chain exceeds max depth 3") {
		t.Errorf("depth-4 chain not rejected: %v", r.Errors)
	}

	// Three edges is legal.
	p, r = Load(fsOf(map[string]string{
		"b.yaml": minimal("b", "c", ""),
		"c.yaml": minimal("c", "d", ""),
		"d.yaml": minimal("d", "e", ""),
		"e.yaml": minimal("e", "", ""),
	}))
	Validate(p, r)
	if errsContain(r, "extends chain") {
		t.Errorf("depth-3 chain wrongly rejected: %v", r.Errors)
	}
}

func TestExtendsUnknownParent(t *testing.T) {
	p, r := Load(fsOf(map[string]string{"a.yaml": minimal("a", "ghost", "")}))
	Validate(p, r)
	if !errsContain(r, `extends unknown template "ghost"`) {
		t.Errorf("unknown parent not reported: %v", r.Errors)
	}
}

func TestDuplicateID(t *testing.T) {
	p, r := Load(fsOf(map[string]string{
		"one.yaml": minimal("same", "", ""),
		"two.yaml": minimal("same", "", ""),
	}))
	Validate(p, r)
	if !errsContain(r, `duplicate template id "same"`) {
		t.Errorf("duplicate id not reported: %v", r.Errors)
	}
}

func TestRemovalOfMissingParentItemWarns(t *testing.T) {
	child := minimal("child", "parent", `  - id: never-existed
    name: Ghost
    category: food
    quantity: 0
    unit: ea
`)
	p, r := Load(fsOf(map[string]string{
		"parent.yaml": minimal("parent", "", ""),
		"child.yaml":  child,
	}))
	Validate(p, r)
	if !warnsContain(r, `"never-existed" has quantity 0 (removal) but is not present in parent`) {
		t.Errorf("phantom removal not warned: warnings=%v errors=%v", r.Warnings, r.Errors)
	}
	if errsContain(r, "quantity 0") {
		t.Errorf("quantity 0 in a child must not be an error: %v", r.Errors)
	}
}

func TestSelfSubstitute(t *testing.T) {
	tpl := minimal("selfsub", "", `  - id: narcissist
    name: Self Sub
    category: tools
    quantity: 1
    unit: ea
    substitutes: [narcissist]
`)
	p, r := Load(fsOf(map[string]string{"t.yaml": tpl}))
	Validate(p, r)
	if !errsContain(r, `"narcissist" lists itself as a substitute`) {
		t.Errorf("self-substitute not rejected: %v", r.Errors)
	}
}

func TestMissingLicenseRefusal(t *testing.T) {
	// Valid in every way except the absent source block.
	tpl := strings.Replace(minimal("nolicense", "", ""), "source:\n  author: test\n  license: CC0-1.0\n", "", 1)
	p, r := Load(fsOf(map[string]string{"t.yaml": tpl}))
	Validate(p, r)
	if !errsContain(r, "no source.license declared") {
		t.Errorf("unlicensed template not refused: %v", r.Errors)
	}
}

func TestSingleMemberRedundancyGroupWarns(t *testing.T) {
	tpl := minimal("lonely", "", `  - id: only-light
    name: Only Light
    category: power
    quantity: 1
    unit: ea
    redundancy_group: lighting
`)
	p, r := Load(fsOf(map[string]string{"t.yaml": tpl}))
	Validate(p, r)
	if !warnsContain(r, `redundancy_group "lighting" has only one member`) {
		t.Errorf("single-member group not warned: %v", r.Warnings)
	}
}

func TestConsumableWithNoReminderWarns(t *testing.T) {
	tpl := minimal("silent", "", `  - id: silent-consumable
    name: Silent
    category: food
    quantity: 1
    unit: ea
    consumable:
      rotation_policy: fifo
`)
	p, r := Load(fsOf(map[string]string{"t.yaml": tpl}))
	Validate(p, r)
	if !warnsContain(r, `"silent-consumable" is consumable but declares neither`) {
		t.Errorf("reminder-less consumable not warned: %v", r.Warnings)
	}
}

func TestOverrideCountsAgainstChildNotDuplicate(t *testing.T) {
	// A child overriding base-item must produce one line, not two, and the
	// child's values must win.
	child := minimal("kid", "papa", `  - id: base-item
    name: Overridden Item
    category: water
    quantity: 9
    unit: ea
`)
	// minimal() puts base-item first and the extra after; here the extra IS
	// base-item again, which within one file is fine for this test's purpose
	// only if we drop the duplicate own line. Build the child by hand
	// instead.
	child = `spec_version: "0.1"
id: kid
name: Kid
layer: home
extends: papa
items:
  - id: base-item
    name: Overridden Item
    category: water
    quantity: 9
    unit: ea
source:
  author: test
  license: CC0-1.0
`
	p, r := Load(fsOf(map[string]string{
		"papa.yaml": minimal("papa", "", ""),
		"kid.yaml":  child,
	}))
	Validate(p, r)
	if !r.OK() {
		t.Fatalf("unexpected errors: %v", r.Errors)
	}
	resolved := p.ByID["kid"].Resolved
	if len(resolved) != 1 {
		t.Fatalf("override produced %d lines, want 1", len(resolved))
	}
	if resolved[0].Quantity != 9 || resolved[0].Name != "Overridden Item" {
		t.Errorf("child override did not win: %+v", resolved[0])
	}
}
