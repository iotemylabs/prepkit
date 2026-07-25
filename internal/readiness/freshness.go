package readiness

import (
	"fmt"
	"sort"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
)

// freshness computes the rotation and verification section: expiring soon,
// expired-but-present, checks overdue, and whether the kit itself has been
// physically verified recently enough. Coverage is not readiness; these are
// reported separately by design.
func freshness(in Input, itemsByLine map[string][]model.Item, lotsByItem map[string][]model.Lot, today time.Time, cfg Config, r *Report) Freshness {
	f := Freshness{LastVerified: in.Kit.LastVerified}

	itemName := map[string]string{}
	itemArchived := map[string]bool{}
	for _, it := range in.UserItems {
		itemName[it.ID] = it.Name
		itemArchived[it.ID] = it.Archived
	}

	// Lot dates: expired, expiring soon, and per-lot next_check.
	for _, lot := range in.Lots {
		if itemArchived[lot.ItemID] || !lot.EffectiveStatus().CountsTowardStock() {
			continue
		}
		if lot.Expires != "" {
			d, err := model.ParseDate(lot.Expires)
			if err != nil {
				r.Warnings = append(r.Warnings, fmt.Sprintf(
					"lot of %q has unparseable expires %q; it is being treated as undated", itemName[lot.ItemID], lot.Expires))
			} else {
				days := int(d.Sub(today).Hours() / 24)
				flag := LotFlag{
					LotID: lot.ID, ItemID: lot.ItemID, ItemName: itemName[lot.ItemID],
					Quantity: lot.Quantity, Expires: lot.Expires,
					ExpiryBasis: lot.ExpiryBasis, DaysUntil: days,
				}
				switch {
				case d.Before(today):
					f.Expired = append(f.Expired, flag)
				case days <= cfg.ExpiringHorizonDays:
					f.ExpiringSoon = append(f.ExpiringSoon, flag)
				}
			}
		}
		if lot.NextCheck != "" {
			d, err := model.ParseDate(lot.NextCheck)
			if err != nil {
				r.Warnings = append(r.Warnings, fmt.Sprintf(
					"lot of %q has unparseable next_check %q", itemName[lot.ItemID], lot.NextCheck))
			} else if d.Before(today) {
				f.CheckOverdue = append(f.CheckOverdue, CheckFlag{
					ItemID: lot.ItemID, ItemName: itemName[lot.ItemID], LotID: lot.ID,
					Due:    lot.NextCheck,
					Reason: "next_check date has passed",
				})
			}
		}
	}

	// Interval-based checks from the template's consumable profiles. This
	// is the only mechanism that catches degrades_without_expiry items.
	// Anchor precedence (approved design): the item's most recent
	// qualifying maintenance event, else its acquired date, else it has
	// never been checked and is overdue outright. A kit-wide event (kit_id
	// set, no item or lot) counts for every item in the kit: a repack
	// touched everything.
	flaggedItems := map[string]bool{}
	for _, c := range f.CheckOverdue {
		flaggedItems[c.ItemID] = true
	}
	for _, tl := range in.Items {
		if tl.Consumable == nil || tl.Consumable.CheckIntervalMonths <= 0 {
			continue
		}
		interval := tl.Consumable.CheckIntervalMonths
		for _, it := range itemsByLine[tl.ID] {
			if flaggedItems[it.ID] {
				continue // already flagged via a lot's next_check
			}
			anchor, ok := latestCheckAnchor(it, lotsByItem[it.ID], in.Events, in.Kit.ID)
			if !ok {
				f.CheckOverdue = append(f.CheckOverdue, CheckFlag{
					ItemID: it.ID, ItemName: it.Name,
					Reason: fmt.Sprintf("never checked; the template asks for inspection every %g months", interval),
				})
				continue
			}
			due := addMonths(anchor, interval)
			if due.Before(today) {
				f.CheckOverdue = append(f.CheckOverdue, CheckFlag{
					ItemID: it.ID, ItemName: it.Name,
					Due:    due.Format(model.DateLayout),
					Reason: fmt.Sprintf("last checked %s; the template asks for inspection every %g months", anchor.Format(model.DateLayout), interval),
				})
			}
		}
	}

	// Kit verification. last_verified is the last time a human physically
	// opened the kit — a database that has drifted from the actual bag is
	// the most common real failure, so this is weighted prominently.
	switch {
	case in.Kit.LastVerified == "":
		f.Unverified = true
		f.UnverifiedReason = "this kit has never been physically verified"
	default:
		d, err := model.ParseDate(in.Kit.LastVerified)
		if err != nil {
			f.Unverified = true
			f.UnverifiedReason = fmt.Sprintf("last_verified %q is unparseable", in.Kit.LastVerified)
		} else if addMonths(d, cfg.UnverifiedAfterMonths).Before(today) {
			f.Unverified = true
			f.UnverifiedReason = fmt.Sprintf(
				"last physically verified %s, more than %g months ago", in.Kit.LastVerified, cfg.UnverifiedAfterMonths)
		}
	}

	sortLotFlags(f.Expired)
	sortLotFlags(f.ExpiringSoon)
	sort.Slice(f.CheckOverdue, func(i, j int) bool {
		a, b := f.CheckOverdue[i], f.CheckOverdue[j]
		if a.ItemID != b.ItemID {
			return a.ItemID < b.ItemID
		}
		return a.LotID < b.LotID
	})
	return f
}

func sortLotFlags(flags []LotFlag) {
	sort.Slice(flags, func(i, j int) bool {
		a, b := flags[i], flags[j]
		if a.Expires != b.Expires {
			return a.Expires < b.Expires
		}
		return a.LotID < b.LotID
	})
}

// latestCheckAnchor finds the date the item was last verifiably attended to:
// the newest maintenance event naming the item, one of its lots, or the
// whole kit; falling back to the acquisition date. ok is false when nothing
// anchors the clock at all.
func latestCheckAnchor(it model.Item, lots []model.Lot, events []model.MaintenanceEvent, kitID string) (time.Time, bool) {
	lotIDs := map[string]bool{}
	for _, l := range lots {
		lotIDs[l.ID] = true
	}
	var best time.Time
	found := false
	consider := func(dateStr string) {
		d, err := model.ParseDate(dateStr)
		if err != nil {
			return
		}
		if !found || d.After(best) {
			best, found = d, true
		}
	}
	for _, ev := range events {
		switch {
		case ev.ItemID == it.ID:
			consider(ev.Date)
		case ev.LotID != "" && lotIDs[ev.LotID]:
			consider(ev.Date)
		case ev.KitID != "" && ev.KitID == kitID && ev.ItemID == "" && ev.LotID == "":
			consider(ev.Date)
		}
	}
	if !found && it.Acquired != "" {
		consider(it.Acquired)
	}
	return best, found
}

// addMonths advances by a possibly fractional number of months: whole months
// via calendar arithmetic, the remainder as mean-month days. Template check
// intervals are typically whole months; the fraction path keeps fractional
// schema values honest.
func addMonths(t time.Time, months float64) time.Time {
	whole := int(months)
	out := t.AddDate(0, whole, 0)
	if frac := months - float64(whole); frac > 0 {
		out = out.Add(time.Duration(frac * 30.44 * 24 * float64(time.Hour)))
	}
	return out
}

// weightReport estimates carried weight against a declared ceiling, when the
// template declares one. Per docs/readiness.md the estimate sums weight
// across counted user items, falling back to the template's per-line weight
// where an item has no weight of its own; when a non-optional counted item
// has no weight anywhere the estimate is presented as a floor, since a
// partial sum that looks comfortably under the ceiling is misleading.
func weightReport(in Input, itemsByLine map[string][]model.Item, lotsByItem map[string][]model.Lot) *WeightReport {
	if in.Template.Container == nil || in.Template.Container.TargetWeightKg <= 0 {
		return nil
	}
	w := &WeightReport{CeilingKg: in.Template.Container.TargetWeightKg}
	totalG := 0.0
	for _, tl := range in.Items {
		countable := tl.Unit == model.UnitEa || tl.Unit == model.UnitPair || tl.Unit == model.UnitSet
		for _, it := range itemsByLine[tl.ID] {
			qty := physicalQuantity(it, lotsByItem[it.ID])
			if qty == 0 {
				continue
			}
			grams := it.WeightG
			if grams == nil {
				grams = tl.WeightG
			}
			if grams == nil {
				if tl.EffectiveCriticality() != model.CritOptional {
					w.UnknownItems = append(w.UnknownItems, it.Name)
					w.IsFloor = true
				}
				continue
			}
			if countable {
				totalG += *grams * qty
			} else {
				totalG += *grams
			}
		}
	}
	w.EstimateKg = totalG / 1000
	w.OverCeiling = w.EstimateKg > w.CeilingKg
	return w
}

// physicalQuantity is what is actually in the bag: countable lots regardless
// of expiry (an expired can still weighs what it weighs), or the item's own
// quantity when it has no lots.
func physicalQuantity(it model.Item, lots []model.Lot) float64 {
	if it.Archived {
		return 0
	}
	if len(lots) == 0 {
		if it.Quantity != nil {
			return *it.Quantity
		}
		return 0
	}
	total := 0.0
	for _, l := range lots {
		if l.EffectiveStatus().CountsTowardStock() {
			total += l.Quantity
		}
	}
	return total
}

// assumptionWarnings compares the household profile against the template's
// stated assumptions. Scaling multiplies by headcount and days only;
// silently rescaling a temperate template for an arctic household produces a
// confident number that is wrong, so material differences attach warnings.
// (Mobility is an assumption too, but the household profile records no
// mobility, so there is nothing to compare it against.)
func assumptionWarnings(a *model.Assumptions, h *model.Household) []string {
	if h == nil {
		return nil
	}
	var out []string
	if tc := a.EffectiveClimate(); tc != model.ClimateAny && h.Climate != "" && h.Climate != tc {
		out = append(out, fmt.Sprintf(
			"household climate %q differs from the template's assumed climate %q; scaling adjusts for headcount and duration only, not climate — review quantities for fit", h.Climate, tc))
	}
	if te := a.EffectiveEnvironment(); te != model.EnvAny && h.Environment != "" && h.Environment != te {
		out = append(out, fmt.Sprintf(
			"household environment %q differs from the template's assumed environment %q; review the kit for fit", h.Environment, te))
	}
	return out
}
