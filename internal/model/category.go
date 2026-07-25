package model

import "slices"

// Category is the functional category of an item. Categorize by function in
// use, not by material or storage location: a metal water bottle is water,
// a lighter is fire even if it lives in the tool pouch.
type Category string

const (
	CatWater      Category = "water"
	CatFood       Category = "food"
	CatShelter    Category = "shelter"
	CatFirstAid   Category = "first_aid"
	CatComms      Category = "comms"
	CatFire       Category = "fire"
	CatTools      Category = "tools"
	CatHygiene    Category = "hygiene"
	CatClothing   Category = "clothing"
	CatPersonal   Category = "personal"
	CatCooking    Category = "cooking"
	CatPower      Category = "power"
	CatDocuments  Category = "documents"
	CatNavigation Category = "navigation"
	CatSecurity   Category = "security"
)

var Categories = []Category{
	CatWater, CatFood, CatShelter, CatFirstAid, CatComms,
	CatFire, CatTools, CatHygiene, CatClothing, CatPersonal,
	CatCooking, CatPower, CatDocuments, CatNavigation, CatSecurity,
}

func (v Category) Valid() bool { return slices.Contains(Categories, v) }

// categoryColors is the canonical mapping from docs/categories.md. Colour is
// presentation, not data: templates declare only the category, and this map
// is applied at display time. The four spec additions (power, documents,
// navigation, security) have no assigned colour and are rendered neutrally.
var categoryColors = map[Category]string{
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

// Color returns the canonical display colour for the category. ok is false
// for the four uncoloured categories (and invalid values), which consumers
// render neutrally.
func (v Category) Color() (color string, ok bool) {
	color, ok = categoryColors[v]
	return color, ok
}
