package template

import (
	"slices"
	"strings"

	"github.com/iotemylabs/prepkit/internal/model"
)

// crossFieldChecks ports validate.py's cross-field rules, plus the licensing
// rule from the build brief that validate.py does not yet enforce: a template
// with no source.license must not load.
func crossFieldChecks(l *Loaded, r *Report) {
	file := l.File
	items := l.Resolved

	// No license, no load. See docs/licensing.md.
	if l.Tpl.Source == nil || strings.TrimSpace(l.Tpl.Source.License) == "" {
		r.errf(file, "no source.license declared; templates without a license do not load (see docs/licensing.md)")
	}

	// quantity 0 is only legal as a removal directive in a child template.
	if l.Tpl.Extends == "" {
		for _, it := range l.Tpl.Items {
			if it.Quantity == 0 {
				r.errf(file, "item %q has quantity 0 in a base template; 0 is only meaningful as a removal in a child", it.ID)
			}
		}
	}

	// substitutes must point at real items in the resolved set.
	ids := make(map[string]bool, len(items))
	for _, it := range items {
		ids[it.ID] = true
	}
	for _, it := range items {
		for _, sub := range it.Substitutes {
			if !ids[sub] {
				r.errf(file, "item %q lists substitute %q which does not exist", it.ID, sub)
			}
		}
		if slices.Contains(it.Substitutes, it.ID) {
			r.errf(file, "item %q lists itself as a substitute", it.ID)
		}
	}

	// A redundancy group with one member is a modelling mistake, not an error.
	groups := map[string][]string{}
	for _, it := range items {
		if g := it.RedundancyGroup; g != "" {
			groups[g] = append(groups[g], it.ID)
		}
	}
	for g, members := range groups {
		if len(members) < 2 {
			r.warnf(file, "redundancy_group %q has only one member (%s); groups exist to express alternative means to one capability", g, members[0])
		}
	}

	// edc_tier is only meaningful on the self layer. The schema also rejects
	// this; the duplicate exists to emit a readable message.
	if l.Tpl.EDCTier != 0 && l.Tpl.Layer != model.LayerSelf {
		r.errf(file, "edc_tier set but layer is %q, not \"self\"", l.Tpl.Layer)
	}

	// Weight over a declared ceiling is a warning; the estimate itself is
	// computed in summarize so the CLI can print it either way.
	if c := l.Tpl.Container; c != nil && c.TargetWeightKg > 0 {
		kg, unknown := estimateWeight(items)
		if kg > c.TargetWeightKg {
			r.warnf(file, "estimated weight %.2f kg exceeds declared target_weight_kg of %g kg", kg, c.TargetWeightKg)
		}
		if len(unknown) > 0 {
			shown := unknown
			ellipsis := ""
			if len(shown) > 5 {
				shown, ellipsis = shown[:5], " ..."
			}
			r.warnf(file, "%d non-optional items lack weight_g, so the estimate is a floor: %s%s",
				len(unknown), strings.Join(shown, ", "), ellipsis)
		}
	}

	// Consumables should say how they are checked.
	for _, it := range items {
		c := it.Consumable
		if c == nil {
			continue
		}
		if c.ShelfLifeMonths == 0 && c.CheckIntervalMonths == 0 {
			r.warnf(file, "item %q is consumable but declares neither shelf_life_months nor check_interval_months, so nothing will remind you", it.ID)
		}
		if c.DegradesWithoutDate && c.CheckIntervalMonths == 0 {
			r.errf(file, "item %q degrades_without_expiry but has no check_interval_months; there is no other way to surface it", it.ID)
		}
	}
}
