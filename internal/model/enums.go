package model

import "slices"

// Layer is a ring of the concentric preparedness model, from carried-on-body
// outward.
type Layer string

const (
	LayerSelf       Layer = "self"
	LayerHome       Layer = "home"
	LayerVehicle    Layer = "vehicle"
	LayerOffsite    Layer = "offsite"
	LayerEvacuation Layer = "evacuation"
)

var Layers = []Layer{LayerSelf, LayerHome, LayerVehicle, LayerOffsite, LayerEvacuation}

func (v Layer) Valid() bool { return slices.Contains(Layers, v) }

// Duration is the sustainment tier a kit is sized for.
type Duration string

const (
	Duration72h        Duration = "72h"
	Duration2w         Duration = "2w"
	Duration1m         Duration = "1m"
	Duration3m         Duration = "3m"
	Duration1y         Duration = "1y"
	DurationIndefinite Duration = "indefinite"
)

var Durations = []Duration{Duration72h, Duration2w, Duration1m, Duration3m, Duration1y, DurationIndefinite}

func (v Duration) Valid() bool { return slices.Contains(Durations, v) }

// Days returns the duration-to-days mapping defined in docs/readiness.md.
// ok is false for "indefinite" (and the empty value), which has no defined
// day count: per_person_per_day lines in an indefinite kit are reported as
// unscored rather than given an invented number.
func (v Duration) Days() (days float64, ok bool) {
	switch v {
	case Duration72h:
		return 3, true
	case Duration2w:
		return 14, true
	case Duration1m:
		return 30, true
	case Duration3m:
		return 90, true
	case Duration1y:
		return 365, true
	default:
		return 0, false
	}
}

// EvacuationTier classifies kits intended to leave the home base.
type EvacuationTier string

const (
	EvacShortTerm EvacuationTier = "short_term"
	EvacLongTerm  EvacuationTier = "long_term"
	EvacINCH      EvacuationTier = "inch"
)

var EvacuationTiers = []EvacuationTier{EvacShortTerm, EvacLongTerm, EvacINCH}

func (v EvacuationTier) Valid() bool { return slices.Contains(EvacuationTiers, v) }

// Criticality weights an item for readiness scoring. The three levels are
// always reported separately; see docs/readiness.md.
type Criticality string

const (
	CritEssential   Criticality = "essential"
	CritRecommended Criticality = "recommended"
	CritOptional    Criticality = "optional"
)

var Criticalities = []Criticality{CritEssential, CritRecommended, CritOptional}

func (v Criticality) Valid() bool { return slices.Contains(Criticalities, v) }

// Unit is the unit enum for template quantities. Inventory items carry
// free-form unit strings; the enum constrains templates only.
type Unit string

const (
	UnitEa   Unit = "ea"
	UnitPair Unit = "pair"
	UnitSet  Unit = "set"
	UnitL    Unit = "l"
	UnitMl   Unit = "ml"
	UnitGal  Unit = "gal"
	UnitFlOz Unit = "floz"
	UnitKg   Unit = "kg"
	UnitG    Unit = "g"
	UnitLb   Unit = "lb"
	UnitOz   Unit = "oz"
	UnitKcal Unit = "kcal"
	UnitM    Unit = "m"
	UnitFt   Unit = "ft"
	UnitDay  Unit = "day"
	UnitDose Unit = "dose"
)

var Units = []Unit{
	UnitEa, UnitPair, UnitSet, UnitL, UnitMl, UnitGal, UnitFlOz,
	UnitKg, UnitG, UnitLb, UnitOz, UnitKcal, UnitM, UnitFt, UnitDay, UnitDose,
}

func (v Unit) Valid() bool { return slices.Contains(Units, v) }

// Scaling says how a template quantity multiplies against a household
// profile. Multipliers are defined in docs/readiness.md.
type Scaling string

const (
	ScaleFixed           Scaling = "fixed"
	ScalePerPerson       Scaling = "per_person"
	ScalePerPersonPerDay Scaling = "per_person_per_day"
	ScalePerPet          Scaling = "per_pet"
	ScalePerVehicle      Scaling = "per_vehicle"
	ScalePerHousehold    Scaling = "per_household"
)

var Scalings = []Scaling{
	ScaleFixed, ScalePerPerson, ScalePerPersonPerDay,
	ScalePerPet, ScalePerVehicle, ScalePerHousehold,
}

func (v Scaling) Valid() bool { return slices.Contains(Scalings, v) }

// RotationPolicy says how a consumable is kept fresh.
type RotationPolicy string

const (
	RotateFIFO            RotationPolicy = "fifo"
	RotateUseAndReplace   RotationPolicy = "use_and_replace"
	RotateTestAndReplace  RotationPolicy = "test_and_replace"
	RotateReplaceOnExpiry RotationPolicy = "replace_on_expiry"
)

var RotationPolicies = []RotationPolicy{
	RotateFIFO, RotateUseAndReplace, RotateTestAndReplace, RotateReplaceOnExpiry,
}

func (v RotationPolicy) Valid() bool { return slices.Contains(RotationPolicies, v) }

// ContainerType is shared by template container specs and real containers.
type ContainerType string

const (
	ContBackpack ContainerType = "backpack"
	ContSling    ContainerType = "sling"
	ContPouch    ContainerType = "pouch"
	ContTote     ContainerType = "tote"
	ContBin      ContainerType = "bin"
	ContDuffel   ContainerType = "duffel"
	ContCase     ContainerType = "case"
	ContTin      ContainerType = "tin"
	ContVehicle  ContainerType = "vehicle"
	ContRoom     ContainerType = "room"
	ContShelf    ContainerType = "shelf"
	ContOther    ContainerType = "other"
)

var ContainerTypes = []ContainerType{
	ContBackpack, ContSling, ContPouch, ContTote, ContBin, ContDuffel,
	ContCase, ContTin, ContVehicle, ContRoom, ContShelf, ContOther,
}

func (v ContainerType) Valid() bool { return slices.Contains(ContainerTypes, v) }

// Climate, Environment, and Mobility describe template assumptions and
// household profiles. "any" is legal only on templates; a real household is
// somewhere in particular, and the store layer rejects it there.
type Climate string

const (
	ClimateTemperate Climate = "temperate"
	ClimateCold      Climate = "cold"
	ClimateHotArid   Climate = "hot_arid"
	ClimateHotHumid  Climate = "hot_humid"
	ClimateAny       Climate = "any"
)

var Climates = []Climate{ClimateTemperate, ClimateCold, ClimateHotArid, ClimateHotHumid, ClimateAny}

func (v Climate) Valid() bool { return slices.Contains(Climates, v) }

type Environment string

const (
	EnvUrban    Environment = "urban"
	EnvSuburban Environment = "suburban"
	EnvRural    Environment = "rural"
	EnvAny      Environment = "any"
)

var Environments = []Environment{EnvUrban, EnvSuburban, EnvRural, EnvAny}

func (v Environment) Valid() bool { return slices.Contains(Environments, v) }

type Mobility string

const (
	MobilityOnFoot     Mobility = "on_foot"
	MobilityVehicle    Mobility = "vehicle"
	MobilityStationary Mobility = "stationary"
)

var Mobilities = []Mobility{MobilityOnFoot, MobilityVehicle, MobilityStationary}

func (v Mobility) Valid() bool { return slices.Contains(Mobilities, v) }

// PermissionStatus records the provenance of a template's source material.
type PermissionStatus string

const (
	PermOriginal          PermissionStatus = "original"
	PermPublicDomain      PermissionStatus = "public_domain"
	PermLicensed          PermissionStatus = "licensed"
	PermPermissionGranted PermissionStatus = "permission_granted"
	PermUnverified        PermissionStatus = "unverified"
)

var PermissionStatuses = []PermissionStatus{
	PermOriginal, PermPublicDomain, PermLicensed, PermPermissionGranted, PermUnverified,
}

func (v PermissionStatus) Valid() bool { return slices.Contains(PermissionStatuses, v) }

// AgeBand is deliberately coarse: enough for kit sizing without carrying a
// birthdate into exports.
type AgeBand string

const (
	AgeInfant AgeBand = "infant"
	AgeChild  AgeBand = "child"
	AgeTeen   AgeBand = "teen"
	AgeAdult  AgeBand = "adult"
	AgeSenior AgeBand = "senior"
)

var AgeBands = []AgeBand{AgeInfant, AgeChild, AgeTeen, AgeAdult, AgeSenior}

func (v AgeBand) Valid() bool { return slices.Contains(AgeBands, v) }

// LotStatus tracks a lot through its life. The zero value means "sealed",
// the schema default.
type LotStatus string

const (
	LotSealed     LotStatus = "sealed"
	LotOpened     LotStatus = "opened"
	LotConsumed   LotStatus = "consumed"
	LotDiscarded  LotStatus = "discarded"
	LotRotatedOut LotStatus = "rotated_out"
)

var LotStatuses = []LotStatus{LotSealed, LotOpened, LotConsumed, LotDiscarded, LotRotatedOut}

func (v LotStatus) Valid() bool { return slices.Contains(LotStatuses, v) }

// CountsTowardStock reports whether a lot in this status is still physically
// present stock. Consumed, discarded, and rotated-out lots never count toward
// coverage (docs/readiness.md).
func (v LotStatus) CountsTowardStock() bool {
	switch v {
	case LotConsumed, LotDiscarded, LotRotatedOut:
		return false
	default: // sealed, opened, or unset (defaults to sealed)
		return true
	}
}

// ExpiryBasis records what a lot's date actually means. "Best by" on rice and
// "use by" on infant formula are different in kind, and reporting
// distinguishes them.
type ExpiryBasis string

const (
	ExpiryPrintedBestBy ExpiryBasis = "printed_best_by"
	ExpiryPrintedUseBy  ExpiryBasis = "printed_use_by"
	ExpiryPrintedExp    ExpiryBasis = "printed_exp"
	ExpiryEstimated     ExpiryBasis = "estimated"
	ExpiryMfgPlusShelf  ExpiryBasis = "manufacture_plus_shelf_life"
)

var ExpiryBases = []ExpiryBasis{
	ExpiryPrintedBestBy, ExpiryPrintedUseBy, ExpiryPrintedExp,
	ExpiryEstimated, ExpiryMfgPlusShelf,
}

func (v ExpiryBasis) Valid() bool { return slices.Contains(ExpiryBases, v) }

// DocumentKind classifies a document. Several kinds are sensitive
// unconditionally; see NeverExportUnencrypted.
type DocumentKind string

const (
	DocPlan         DocumentKind = "plan"
	DocContactSheet DocumentKind = "contact_sheet"
	DocMedical      DocumentKind = "medical"
	DocInsurance    DocumentKind = "insurance"
	DocIdentity     DocumentKind = "identity"
	DocProperty     DocumentKind = "property"
	DocFinancial    DocumentKind = "financial"
	DocManual       DocumentKind = "manual"
	DocReference    DocumentKind = "reference"
	DocOther        DocumentKind = "other"
)

var DocumentKinds = []DocumentKind{
	DocPlan, DocContactSheet, DocMedical, DocInsurance, DocIdentity,
	DocProperty, DocFinancial, DocManual, DocReference, DocOther,
}

func (v DocumentKind) Valid() bool { return slices.Contains(DocumentKinds, v) }

// NeverExportUnencrypted reports whether documents of this kind are barred
// from every unencrypted export, with no opt-in: docs/threat-model.md lists
// identity, financial, insurance, and medical as "never, no exception".
// export_ok cannot override this.
func (v DocumentKind) NeverExportUnencrypted() bool {
	switch v {
	case DocIdentity, DocFinancial, DocInsurance, DocMedical:
		return true
	default:
		return false
	}
}

// MaintenanceKind classifies a physical action taken on a kit, item, or lot.
type MaintenanceKind string

const (
	MaintInspection   MaintenanceKind = "inspection"
	MaintRotation     MaintenanceKind = "rotation"
	MaintReplacement  MaintenanceKind = "replacement"
	MaintFunctionTest MaintenanceKind = "function_test"
	MaintRepack       MaintenanceKind = "repack"
	MaintDrill        MaintenanceKind = "drill"
)

var MaintenanceKinds = []MaintenanceKind{
	MaintInspection, MaintRotation, MaintReplacement,
	MaintFunctionTest, MaintRepack, MaintDrill,
}

func (v MaintenanceKind) Valid() bool { return slices.Contains(MaintenanceKinds, v) }
