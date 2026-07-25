package model

// Inventory mirrors schema/inventory.schema.json: the user's actual holdings,
// as stored by a running instance and produced by a full export. Distinct
// from Template, which is an aspirational standard.
//
// All dates are ISO-8601 strings ("2006-01-02", or RFC 3339 for ExportedAt),
// kept as strings so an export round-trips byte-for-byte; parse with
// ParseDate/ParseDateTime where arithmetic is needed. All IDs are UUID
// strings.
type Inventory struct {
	SpecVersion string             `json:"spec_version"`
	ExportedAt  string             `json:"exported_at"`
	InstanceID  string             `json:"instance_id,omitempty"`
	Household   *Household         `json:"household,omitempty"`
	Containers  []Container        `json:"containers,omitempty"`
	Kits        []Kit              `json:"kits,omitempty"`
	Items       []Item             `json:"items,omitempty"`
	Lots        []Lot              `json:"lots,omitempty"`
	Documents   []Document         `json:"documents,omitempty"`
	Maintenance []MaintenanceEvent `json:"maintenance,omitempty"`
}

// Household drives per_person and per_person_per_day scaling. Deliberately
// minimal: planning input, not a personnel file.
type Household struct {
	Label       string      `json:"label,omitempty"`
	Persons     []Person    `json:"persons,omitempty"`
	Pets        int         `json:"pets,omitempty"`
	Climate     Climate     `json:"climate,omitempty"`
	Environment Environment `json:"environment,omitempty"`
}

// Person is a household member. A role label ("parent 1", "youngest") is
// preferred over a legal name for anything that may be exported.
type Person struct {
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	AgeBand AgeBand `json:"age_band,omitempty"`

	// Needs is SENSITIVE: free-text planning notes (dietary restriction,
	// mobility aid, prescription dependency). Excluded from any unencrypted
	// export by default, opt-in per export, and never logged.
	Needs []string `json:"needs,omitempty"`
}

// Container is a nestable physical vessel. A pouch inside a pack inside a
// vehicle is three containers and two parent edges. Self-referential and
// cyclic parentage are rejected on write by the store layer.
type Container struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	ParentID string        `json:"parent_id,omitempty"`
	Type     ContainerType `json:"type,omitempty"`

	// ColorCode records the real vessel's colour, which may or may not match
	// a category's canonical colour; free-form by design.
	ColorCode string `json:"color_code,omitempty"`

	// LocationNote is sensitive-by-default for export: it maps valuables to
	// places within the home.
	LocationNote string `json:"location_note,omitempty"`

	// QRSlug is the opaque local token printed on the container's label.
	// Never a URL, never anything resolvable off-instance: a label on a bin
	// in a garage is a public object.
	QRSlug string `json:"qr_slug,omitempty"`

	// WeightG is the empty weight of the vessel itself; pointer because 0 is
	// a legal declared weight.
	WeightG *float64 `json:"weight_g,omitempty"`
}

// Kit is a user's real instance of a preparedness kit, optionally scored
// against a template.
type Kit struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// TemplateID/TemplateVersion identify the template this was instantiated
	// from, retained so the kit can be re-scored when the template updates.
	TemplateID      string `json:"template_id,omitempty"`
	TemplateVersion string `json:"template_version,omitempty"`

	Layer            Layer          `json:"layer,omitempty"`
	Duration         Duration       `json:"duration,omitempty"`
	EvacuationTier   EvacuationTier `json:"evacuation_tier,omitempty"`
	ContainerID      string         `json:"container_id,omitempty"`
	AssignedPersonID string         `json:"assigned_person_id,omitempty"`

	// LastVerified is the last time a human physically opened this kit and
	// confirmed contents — distinct from any record edit timestamp. A
	// database that has drifted from the actual bag is the failure this
	// field exists to expose, and reports weight it prominently.
	LastVerified string `json:"last_verified,omitempty"`

	Notes string `json:"notes,omitempty"`
}

// Item is a type of thing the user owns.
type Item struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category Category `json:"category"`

	// TemplateItemID is which template line this satisfies — the link that
	// lets a specific owned product count toward a generic requirement and
	// that survives template upgrades.
	TemplateItemID string `json:"template_item_id,omitempty"`

	KitID       string `json:"kit_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`

	// Quantity is present for non-consumables only. It MUST be nil when the
	// item has lots; the effective quantity is then the sum of lot
	// quantities, so the two can never disagree. The store layer enforces
	// this invariant on write.
	Quantity *float64 `json:"quantity,omitempty"`
	Unit     string   `json:"unit,omitempty"`

	// Product is the actual make and model the user owns; free-form, carries
	// no recommendation.
	Product string `json:"product,omitempty"`

	WeightG  *float64 `json:"weight_g,omitempty"`
	Acquired string   `json:"acquired,omitempty"`

	// Cost is SENSITIVE for export purposes (insurance documentation);
	// pointer because 0 is a legal recorded cost. Never logged.
	Cost *float64 `json:"cost,omitempty"`

	PhotoIDs []string `json:"photo_ids,omitempty"`

	// Serial is SENSITIVE: valuable for insurance claims and theft recovery,
	// and for exactly that reason excluded from unencrypted exports by
	// default and never logged.
	Serial string `json:"serial,omitempty"`

	Notes    string `json:"notes,omitempty"`
	Archived bool   `json:"archived,omitempty"`
}

// Lot is a dated batch of a consumable item. Multiple lots per item is the
// normal case, not an edge case: it is how partial rotation is represented.
type Lot struct {
	ID       string  `json:"id"`
	ItemID   string  `json:"item_id"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit,omitempty"`

	// Expires is the printed date, transcribed as-is. Absent for items that
	// degrade without a printed date; those rely on NextCheck instead.
	Expires     string      `json:"expires,omitempty"`
	ExpiryBasis ExpiryBasis `json:"expiry_basis,omitempty"`

	// Opened: once opened, printed shelf life usually no longer applies.
	Opened string `json:"opened,omitempty"`

	// NextCheck is the next physical inspection due — the primary mechanism
	// for items with no printed date and a secondary one for everything else.
	NextCheck string `json:"next_check,omitempty"`

	// ContainerID overrides the parent item's container when present: lots
	// of one item may sit in different places.
	ContainerID string `json:"container_id,omitempty"`

	Status LotStatus `json:"status,omitempty"`
	Notes  string    `json:"notes,omitempty"`
}

// EffectiveStatus applies the schema default of "sealed".
func (l Lot) EffectiveStatus() LotStatus {
	if l.Status == "" {
		return LotSealed
	}
	return l.Status
}

// Document is critical information. SENSITIVE BY DEFAULT: every field is
// assumed confidential. A document appears in an export only when its own
// ExportOK is true, there is no bulk override, and kinds for which
// NeverExportUnencrypted returns true are excluded from unencrypted exports
// unconditionally.
type Document struct {
	ID    string       `json:"id"`
	Title string       `json:"title"`
	Kind  DocumentKind `json:"kind"`

	// Body is markdown content for documents authored in-app. SENSITIVE:
	// never logged.
	Body string `json:"body,omitempty"`

	FileID    string   `json:"file_id,omitempty"`
	PersonIDs []string `json:"person_ids,omitempty"`

	// ReviewIntervalMonths: plans decay, so they get review reminders exactly
	// as consumables get expiry reminders. 0 means "not declared" (the
	// schema's exclusiveMinimum forbids a literal 0).
	ReviewIntervalMonths float64 `json:"review_interval_months,omitempty"`
	LastReviewed         string  `json:"last_reviewed,omitempty"`

	// ExportOK is the explicit per-document opt-in for offline exports.
	// Defaults false and is serialized explicitly rather than omitted, so an
	// export's consent state is always visible on inspection.
	ExportOK bool `json:"export_ok"`
}

// MaintenanceEvent logs a physical action taken — the audit trail that
// distinguishes a kit genuinely verified last month from one whose record
// merely says so.
type MaintenanceEvent struct {
	ID     string          `json:"id"`
	Date   string          `json:"date"`
	Kind   MaintenanceKind `json:"kind"`
	KitID  string          `json:"kit_id,omitempty"`
	ItemID string          `json:"item_id,omitempty"`
	LotID  string          `json:"lot_id,omitempty"`
	Notes  string          `json:"notes,omitempty"`
}
