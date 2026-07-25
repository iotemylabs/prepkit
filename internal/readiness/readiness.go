// Package readiness scores a user's real kit against a resolved template,
// implementing docs/readiness.md from the PrepKit spec exactly.
//
// The package is pure: no database, no HTTP, no clock. Score takes resolved
// template lines plus inventory structs and a time, and returns a Report.
// That makes it directly testable against fixtures, which matters because it
// is the component where a subtle error produces a confidently wrong answer
// rather than a crash.
//
// The rules the spec is most emphatic about, all honoured here:
//
//   - The three criticality levels are reported separately and NEVER
//     averaged into one percentage. If a caller wants a single headline
//     number, it is the essential ratio alone, labeled as such.
//   - Any uncovered essential line marks the kit incomplete, regardless of
//     percentages.
//   - Expired stock does not count toward coverage but is reported
//     distinctly from missing: "expired water" and "no water" are different
//     situations.
//   - Substitutes are checked before a gap is declared; an item and its
//     substitute together contribute at most 1.0.
//   - Redundancy groups report capability coverage alongside item coverage;
//     members are never collapsed into one scored line.
//   - per_person_per_day lines in an indefinite-duration kit are unscored
//     and say so, rather than being given an invented number.
package readiness

import (
	"fmt"
	"sort"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
)

// Config holds the report horizons. Zero values take the spec defaults.
type Config struct {
	// ExpiringHorizonDays flags lots expiring within this many days.
	// Default 90.
	ExpiringHorizonDays int
	// UnverifiedAfterMonths flags kits whose last_verified is older than
	// this. Default 12.
	UnverifiedAfterMonths float64
}

func (c Config) withDefaults() Config {
	if c.ExpiringHorizonDays == 0 {
		c.ExpiringHorizonDays = 90
	}
	if c.UnverifiedAfterMonths == 0 {
		c.UnverifiedAfterMonths = 12
	}
	return c
}

// Input is everything Score needs. UserItems and Lots are the kit's holdings
// (the caller scopes them); Events is the maintenance log relevant to the
// kit and its items.
type Input struct {
	Template  model.Template       // header: assumptions, duration, container
	Items     []model.TemplateItem // resolved lines, in template order
	Household *model.Household
	Kit       model.Kit
	UserItems []model.Item
	Lots      []model.Lot
	Events    []model.MaintenanceEvent
	Now       time.Time
}

// LineStatus classifies one template line's coverage.
type LineStatus string

const (
	// StatusCovered: valid stock meets or exceeds the scaled target.
	StatusCovered LineStatus = "covered"
	// StatusPartial: some valid stock, below target. Counts as uncovered.
	StatusPartial LineStatus = "partial"
	// StatusMissing: no valid stock (possibly expired stock; see
	// ExpiredQuantity).
	StatusMissing LineStatus = "missing"
	// StatusCoveredBySubstitute: own stock insufficient but a listed
	// substitute line is itself fully satisfied.
	StatusCoveredBySubstitute LineStatus = "covered_by_substitute"
	// StatusUnscored: the line cannot be scored (per_person_per_day with no
	// finite duration). Excluded from totals; listed so the report says so.
	StatusUnscored LineStatus = "unscored"
	// StatusNotApplicable: the scaled target is zero (per_pet with no
	// pets). Excluded from totals.
	StatusNotApplicable LineStatus = "not_applicable"
)

// Line is the scored result for one template line.
type Line struct {
	TemplateItemID string
	Name           string
	Category       model.Category
	Criticality    model.Criticality
	Unit           model.Unit
	ScaledTarget   float64
	ValidQuantity  float64
	// ExpiredQuantity is stock physically present but past its date. It
	// contributes nothing to coverage and is reported so "expired" and
	// "missing" stay distinguishable.
	ExpiredQuantity float64
	Ratio           float64 // min(1, valid/target)
	Status          LineStatus
	CoveredBy       string // substitute template item id when covered via substitute
	UnscoredReason  string
	Warnings        []string
}

// Coverage counts covered lines at one criticality level.
type Coverage struct {
	Covered int
	Total   int
}

// Fraction returns covered/total; ok is false when the level has no lines.
func (c Coverage) Fraction() (float64, bool) {
	if c.Total == 0 {
		return 0, false
	}
	return float64(c.Covered) / float64(c.Total), true
}

// GroupReport is capability coverage for one redundancy group.
type GroupReport struct {
	Name           string
	Members        []string // template item ids
	MembersCovered int
	Covered        bool // at least one member covered
	Redundant      bool // every member covered
}

// LotFlag identifies a lot needing attention on the rotation dashboard.
type LotFlag struct {
	LotID       string
	ItemID      string
	ItemName    string
	Quantity    float64
	Expires     string
	ExpiryBasis model.ExpiryBasis
	DaysUntil   int
}

// CheckFlag identifies an item or lot whose physical inspection is overdue.
type CheckFlag struct {
	ItemID   string
	ItemName string
	LotID    string // set when the flag came from a lot's next_check
	Due      string // the date the check was due, when known
	Reason   string
}

// Freshness is the part of readiness that coverage arithmetic cannot see.
type Freshness struct {
	ExpiringSoon []LotFlag
	Expired      []LotFlag
	CheckOverdue []CheckFlag
	// Unverified: the kit has not been physically opened and confirmed
	// within the configured interval. A kit at 100% coverage that nobody
	// has opened in three years is not ready, and the report weights this
	// prominently.
	Unverified       bool
	UnverifiedReason string
	LastVerified     string
}

// WeightReport compares carried weight against a declared ceiling.
type WeightReport struct {
	EstimateKg float64
	CeilingKg  float64
	// IsFloor: at least one non-optional counted item had no weight
	// anywhere, so the estimate understates reality and must be presented
	// as a floor, not a total.
	IsFloor      bool
	UnknownItems []string
	OverCeiling  bool
}

// Report is the full scored result. It is data only: presentation (colors,
// the mandatory "a score is not a safety guarantee" disclaimer) belongs to
// consumers.
type Report struct {
	KitID      string
	KitName    string
	TemplateID string

	// Incomplete is set by any uncovered essential line, regardless of
	// percentages.
	Incomplete bool

	// The three levels are separate figures by design. Never combine them.
	Essential   Coverage
	Recommended Coverage
	Optional    Coverage

	Lines     []Line
	Groups    []GroupReport
	Freshness Freshness
	Weight    *WeightReport // nil when the template declares no ceiling

	// Warnings carries assumption mismatches and data problems (unparseable
	// dates). Rescaling does not adjust for climate or environment, and
	// silence would produce a confident wrong answer.
	Warnings []string

	// Skills passes the template's skill list through for display: gear
	// without skill is a known failure mode.
	Skills []model.SkillRef
}

// EssentialFraction is the only legitimate single headline figure, and it
// must be labeled as the essential ratio wherever shown. Recommended and
// optional coverage never raise it.
func (r *Report) EssentialFraction() (float64, bool) {
	return r.Essential.Fraction()
}

// Score evaluates the kit against the template. See the package comment for
// the rules.
func Score(in Input, cfg Config) *Report {
	cfg = cfg.withDefaults()
	today := in.Now.UTC().Truncate(24 * time.Hour)

	r := &Report{
		KitID:      in.Kit.ID,
		KitName:    in.Kit.Name,
		TemplateID: in.Template.ID,
		Skills:     in.Template.Skills,
	}

	persons, pets := householdCounts(in.Household)
	duration := in.Kit.Duration
	if duration == "" {
		duration = in.Template.Duration
	}

	lotsByItem := map[string][]model.Lot{}
	for _, l := range in.Lots {
		lotsByItem[l.ItemID] = append(lotsByItem[l.ItemID], l)
	}
	itemsByLine := map[string][]model.Item{}
	for _, it := range in.UserItems {
		if it.Archived || it.TemplateItemID == "" {
			continue
		}
		itemsByLine[it.TemplateItemID] = append(itemsByLine[it.TemplateItemID], it)
	}

	// Pass 1: score each line on its own stock.
	for _, tl := range in.Items {
		line := Line{
			TemplateItemID: tl.ID,
			Name:           tl.Name,
			Category:       tl.Category,
			Criticality:    tl.EffectiveCriticality(),
			Unit:           tl.Unit,
		}

		target, scored, reason := scaledTarget(tl, persons, pets, duration)
		if !scored {
			line.Status = StatusUnscored
			line.UnscoredReason = reason
			r.Lines = append(r.Lines, line)
			continue
		}
		line.ScaledTarget = target
		if target == 0 {
			line.Status = StatusNotApplicable
			r.Lines = append(r.Lines, line)
			continue
		}

		valid, expired, warnings := stock(tl, itemsByLine[tl.ID], lotsByItem, today)
		line.ValidQuantity = valid
		line.ExpiredQuantity = expired
		line.Warnings = warnings
		line.Ratio = valid / target
		if line.Ratio > 1 {
			line.Ratio = 1
		}
		switch {
		case line.Ratio >= 1:
			line.Status = StatusCovered
		case line.Ratio > 0:
			line.Status = StatusPartial
		default:
			line.Status = StatusMissing
		}
		r.Lines = append(r.Lines, line)
	}

	// Pass 2: substitutes. An unsatisfied line whose listed substitute is
	// itself fully satisfied counts as covered; no fractional stacking, so
	// the pair contributes at most 1.0.
	ownRatio := map[string]float64{}
	for _, l := range r.Lines {
		ownRatio[l.TemplateItemID] = l.Ratio
	}
	for i := range r.Lines {
		line := &r.Lines[i]
		if line.Status != StatusMissing && line.Status != StatusPartial {
			continue
		}
		tl := in.Items[i]
		for _, sub := range tl.Substitutes {
			if ownRatio[sub] >= 1 {
				line.Status = StatusCoveredBySubstitute
				line.CoveredBy = sub
				break
			}
		}
	}

	// Totals per criticality level, kept separate. Unscored and
	// not-applicable lines are excluded: they are neither covered nor
	// uncovered, and inventing either answer would distort the figures.
	for _, l := range r.Lines {
		if l.Status == StatusUnscored || l.Status == StatusNotApplicable {
			continue
		}
		covered := l.Status == StatusCovered || l.Status == StatusCoveredBySubstitute
		var c *Coverage
		switch l.Criticality {
		case model.CritEssential:
			c = &r.Essential
		case model.CritOptional:
			c = &r.Optional
		default:
			c = &r.Recommended
		}
		c.Total++
		if covered {
			c.Covered++
		} else if l.Criticality == model.CritEssential {
			r.Incomplete = true
		}
	}

	r.Groups = groupReports(in.Items, r.Lines)
	r.Freshness = freshness(in, itemsByLine, lotsByItem, today, cfg, r)
	r.Weight = weightReport(in, itemsByLine, lotsByItem)
	r.Warnings = append(r.Warnings, assumptionWarnings(in.Template.Assumptions, in.Household)...)

	return r
}

// householdCounts extracts scaling multipliers. A household with no persons
// recorded scales as one person: the user exists even if they never filled
// the profile in, and scaling essentials to zero would hide every gap.
func householdCounts(h *model.Household) (persons, pets int) {
	if h == nil {
		return 1, 0
	}
	persons = len(h.Persons)
	if persons == 0 {
		persons = 1
	}
	return persons, h.Pets
}

// scaledTarget resolves a template quantity against the household, per the
// multiplier table in docs/readiness.md. per_vehicle is 1 in Phase 1: the
// schema models no vehicle roster, and a vehicle-layer kit is one vehicle.
func scaledTarget(tl model.TemplateItem, persons, pets int, duration model.Duration) (target float64, scored bool, reason string) {
	switch tl.EffectiveScaling() {
	case model.ScalePerPerson:
		return tl.Quantity * float64(persons), true, ""
	case model.ScalePerPet:
		return tl.Quantity * float64(pets), true, ""
	case model.ScalePerPersonPerDay:
		days, ok := duration.Days()
		if !ok {
			return 0, false, fmt.Sprintf(
				"per_person_per_day line cannot be scored: kit duration is %q, which has no defined day count", string(duration))
		}
		return tl.Quantity * float64(persons) * days, true, ""
	default: // fixed, per_household, per_vehicle
		return tl.Quantity, true, ""
	}
}

// stock sums a line's valid and expired quantities across its linked items.
// Items with lots derive quantity from countable lots; expired lots are
// tallied separately and never count toward coverage. Units must match the
// template line's unit (an unset unit is taken as matching); a mismatch
// excludes the contribution and warns, rather than silently adding gallons
// to litres.
func stock(tl model.TemplateItem, items []model.Item, lotsByItem map[string][]model.Lot, today time.Time) (valid, expired float64, warnings []string) {
	for _, it := range items {
		if it.Unit != "" && it.Unit != string(tl.Unit) {
			warnings = append(warnings, fmt.Sprintf(
				"item %q not counted: unit %q does not match template unit %q", it.Name, it.Unit, tl.Unit))
			continue
		}
		lots := lotsByItem[it.ID]
		if len(lots) == 0 {
			if it.Quantity != nil {
				valid += *it.Quantity
			}
			continue
		}
		for _, lot := range lots {
			if !lot.EffectiveStatus().CountsTowardStock() {
				continue
			}
			if lot.Unit != "" && lot.Unit != string(tl.Unit) {
				warnings = append(warnings, fmt.Sprintf(
					"a lot of %q not counted: unit %q does not match template unit %q", it.Name, lot.Unit, tl.Unit))
				continue
			}
			if isExpired(lot.Expires, today) {
				expired += lot.Quantity
			} else {
				valid += lot.Quantity
			}
		}
	}
	return valid, expired, warnings
}

func isExpired(expires string, today time.Time) bool {
	if expires == "" {
		return false
	}
	d, err := model.ParseDate(expires)
	if err != nil {
		return false // unparseable dates surface via freshness warnings
	}
	return d.Before(today)
}

// groupReports builds capability coverage per redundancy group. Members are
// never collapsed: the group saying "covered" does not change any member
// line's own status.
func groupReports(tls []model.TemplateItem, lines []Line) []GroupReport {
	statusByID := map[string]LineStatus{}
	for _, l := range lines {
		statusByID[l.TemplateItemID] = l.Status
	}
	byName := map[string]*GroupReport{}
	var order []string
	for _, tl := range tls {
		g := tl.RedundancyGroup
		if g == "" {
			continue
		}
		gr, ok := byName[g]
		if !ok {
			gr = &GroupReport{Name: g}
			byName[g] = gr
			order = append(order, g)
		}
		gr.Members = append(gr.Members, tl.ID)
		s := statusByID[tl.ID]
		if s == StatusCovered || s == StatusCoveredBySubstitute {
			gr.MembersCovered++
		}
	}
	sort.Strings(order)
	var out []GroupReport
	for _, name := range order {
		gr := byName[name]
		gr.Covered = gr.MembersCovered >= 1
		gr.Redundant = gr.MembersCovered == len(gr.Members)
		out = append(out, *gr)
	}
	return out
}
