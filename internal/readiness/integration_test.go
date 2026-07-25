package readiness

import (
	"testing"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/internal/template"
	"github.com/iotemylabs/prepkit/templates"
)

// TestAgainstBundledVehicleTemplate scores an empty inventory against the
// real resolved vehicle-kit-baseline (the 21->41 fixture), proving the
// engine consumes actual resolver output: every scored line missing, the
// kit incomplete, and the essential total matching the resolved set.
func TestAgainstBundledVehicleTemplate(t *testing.T) {
	p, r := template.Load(templates.FS)
	template.Validate(p, r)
	if !r.OK() {
		t.Fatalf("bundled templates invalid: %v", r.Errors)
	}
	loaded := p.ByID["vehicle-kit-baseline"]

	now, _ := time.Parse(time.RFC3339, "2026-07-24T12:00:00Z")
	rep := Score(Input{
		Template: loaded.Tpl,
		Items:    loaded.Resolved,
		Kit:      model.Kit{ID: "k1", Name: "My vehicle kit", Duration: model.Duration72h},
		Now:      now,
	}, Config{})

	wantEssential := 0
	for _, tl := range loaded.Resolved {
		if tl.EffectiveCriticality() == model.CritEssential {
			wantEssential++
		}
	}
	if rep.Essential.Total != wantEssential || rep.Essential.Covered != 0 {
		t.Errorf("essential = %d/%d, want 0/%d", rep.Essential.Covered, rep.Essential.Total, wantEssential)
	}
	if !rep.Incomplete {
		t.Error("an empty kit with essential lines must be incomplete")
	}
	if got := len(rep.Lines); got != 41 {
		t.Errorf("scored %d lines, want 41", got)
	}
	if !rep.Freshness.Unverified {
		t.Error("a never-verified kit must report unverified")
	}
	// The vehicle template declares no weight ceiling; no weight report.
	if rep.Weight != nil {
		t.Errorf("unexpected weight report: %+v", rep.Weight)
	}
	// The template's skills pass through for display.
	if len(rep.Skills) != len(loaded.Tpl.Skills) || len(rep.Skills) == 0 {
		t.Errorf("skills not passed through: %d vs %d", len(rep.Skills), len(loaded.Tpl.Skills))
	}
}
