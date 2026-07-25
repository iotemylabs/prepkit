package model

// Template mirrors schema/kit-template.schema.json: a portable description of
// GENERIC item types and target quantities, not the specific products a user
// owns. Templates are pure data — no scripting hooks, no executable content —
// which is the property that makes loading packs contributed by strangers
// safe.
type Template struct {
	SpecVersion    string         `json:"spec_version" yaml:"spec_version"`
	ID             string         `json:"id" yaml:"id"`
	Name           string         `json:"name" yaml:"name"`
	Summary        string         `json:"summary,omitempty" yaml:"summary,omitempty"`
	Extends        string         `json:"extends,omitempty" yaml:"extends,omitempty"`
	Layer          Layer          `json:"layer" yaml:"layer"`
	EDCTier        int            `json:"edc_tier,omitempty" yaml:"edc_tier,omitempty"`
	Duration       Duration       `json:"duration,omitempty" yaml:"duration,omitempty"`
	EvacuationTier EvacuationTier `json:"evacuation_tier,omitempty" yaml:"evacuation_tier,omitempty"`
	Assumptions    *Assumptions   `json:"assumptions,omitempty" yaml:"assumptions,omitempty"`
	Container      *ContainerSpec `json:"container,omitempty" yaml:"container,omitempty"`
	Items          []TemplateItem `json:"items" yaml:"items"`
	Skills         []SkillRef     `json:"skills,omitempty" yaml:"skills,omitempty"`
	Tags           []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
	Source         *Source        `json:"source,omitempty" yaml:"source,omitempty"`
}

// TemplateItem is one line of a template. Its ID is the merge key for
// `extends` resolution and the anchor for template_item_id links from user
// items, which is what lets a template revision avoid orphaning user data.
type TemplateItem struct {
	ID       string      `json:"id" yaml:"id"`
	Name     string      `json:"name" yaml:"name"`
	Category Category    `json:"category" yaml:"category"`
	Quantity float64     `json:"quantity" yaml:"quantity"`
	Unit     Unit        `json:"unit" yaml:"unit"`
	Scaling  Scaling     `json:"scaling,omitempty" yaml:"scaling,omitempty"`
	Crit     Criticality `json:"criticality,omitempty" yaml:"criticality,omitempty"`

	// Consumable is present iff the item expires or degrades; its presence is
	// what makes instantiated instances tracked as dated Lots.
	Consumable *ConsumableProfile `json:"consumable,omitempty" yaml:"consumable,omitempty"`

	Substitutes     []string `json:"substitutes,omitempty" yaml:"substitutes,omitempty"`
	RedundancyGroup string   `json:"redundancy_group,omitempty" yaml:"redundancy_group,omitempty"`

	// WeightG is a typical unit weight in grams, advisory, for pack-weight
	// estimates. A pointer because "no declared weight" and "weighs nothing
	// meaningful" are different facts, and the weight estimate must report
	// itself as a floor when non-optional items lack a weight.
	WeightG *float64 `json:"weight_g,omitempty" yaml:"weight_g,omitempty"`

	Notes          string     `json:"notes,omitempty" yaml:"notes,omitempty"`
	Exemplars      []Exemplar `json:"exemplars,omitempty" yaml:"exemplars,omitempty"`
	RegulatoryNote string     `json:"regulatory_note,omitempty" yaml:"regulatory_note,omitempty"`
}

// EffectiveScaling applies the schema default of "fixed".
func (i TemplateItem) EffectiveScaling() Scaling {
	if i.Scaling == "" {
		return ScaleFixed
	}
	return i.Scaling
}

// EffectiveCriticality applies the schema default of "recommended".
func (i TemplateItem) EffectiveCriticality() Criticality {
	if i.Crit == "" {
		return CritRecommended
	}
	return i.Crit
}

// ConsumableProfile describes how an item expires or degrades. The month
// fields use 0 to mean "not declared": the schema's exclusiveMinimum forbids
// a literal 0, so no legal value is shadowed.
type ConsumableProfile struct {
	ShelfLifeMonths     float64        `json:"shelf_life_months,omitempty" yaml:"shelf_life_months,omitempty"`
	RotationPolicy      RotationPolicy `json:"rotation_policy,omitempty" yaml:"rotation_policy,omitempty"`
	CheckIntervalMonths float64        `json:"check_interval_months,omitempty" yaml:"check_interval_months,omitempty"`
	DegradesWithoutDate bool           `json:"degrades_without_expiry,omitempty" yaml:"degrades_without_expiry,omitempty"`
}

// Exemplar is an illustrative product. ADVISORY ONLY: never presented as a
// required purchase or an endorsement.
type Exemplar struct {
	Description string `json:"description" yaml:"description"`
	Attribution string `json:"attribution,omitempty" yaml:"attribution,omitempty"`
	URL         string `json:"url,omitempty" yaml:"url,omitempty"`
}

// ContainerSpec describes the vessel a template assumes, generically.
type ContainerSpec struct {
	Type ContainerType `json:"type,omitempty" yaml:"type,omitempty"`
	// CapacityL and TargetWeightKg use 0 for "not declared"; the schema's
	// exclusiveMinimum forbids a literal 0.
	CapacityL      float64 `json:"capacity_l,omitempty" yaml:"capacity_l,omitempty"`
	TargetWeightKg float64 `json:"target_weight_kg,omitempty" yaml:"target_weight_kg,omitempty"`
	Notes          string  `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// Assumptions are the conditions a template was sized for. A household
// profile that differs materially must surface a warning rather than silently
// rescale.
type Assumptions struct {
	Persons     int         `json:"persons,omitempty" yaml:"persons,omitempty"`
	Pets        int         `json:"pets,omitempty" yaml:"pets,omitempty"`
	Climate     Climate     `json:"climate,omitempty" yaml:"climate,omitempty"`
	Environment Environment `json:"environment,omitempty" yaml:"environment,omitempty"`
	Mobility    Mobility    `json:"mobility,omitempty" yaml:"mobility,omitempty"`
	Notes       string      `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// EffectiveClimate applies the schema default of "any".
func (a *Assumptions) EffectiveClimate() Climate {
	if a == nil || a.Climate == "" {
		return ClimateAny
	}
	return a.Climate
}

// EffectiveEnvironment applies the schema default of "any".
func (a *Assumptions) EffectiveEnvironment() Environment {
	if a == nil || a.Environment == "" {
		return EnvAny
	}
	return a.Environment
}

// SkillRef is a competency the kit assumes. Gear without skill is a known
// failure mode; templates assert it explicitly and reports surface it.
type SkillRef struct {
	Name  string      `json:"name" yaml:"name"`
	Crit  Criticality `json:"criticality,omitempty" yaml:"criticality,omitempty"`
	Notes string      `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// Source is provenance and license. A template with no license does not load;
// see docs/licensing.md.
type Source struct {
	Author           string           `json:"author" yaml:"author"`
	License          string           `json:"license" yaml:"license"`
	DerivedFrom      string           `json:"derived_from,omitempty" yaml:"derived_from,omitempty"`
	URL              string           `json:"url,omitempty" yaml:"url,omitempty"`
	PermissionStatus PermissionStatus `json:"permission_status,omitempty" yaml:"permission_status,omitempty"`
}
