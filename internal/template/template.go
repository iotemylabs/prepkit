// Package template loads and validates PrepKit template packs.
//
// It is the Go port of the spec repo's validate.py: JSON Schema conformance
// against the embedded kit-template schema, extends resolution, and the
// cross-field rules the schema cannot express. The server runs the same
// checks at startup that `prepkit validate` runs from the CLI, so pack
// authors get identical feedback in both places and a bad pack is rejected
// loudly at load time rather than failing mysteriously later.
package template

import (
	"fmt"
	"io/fs"
	"sort"

	"github.com/iotemylabs/prepkit/internal/model"

	"gopkg.in/yaml.v3"
)

// MaxDepth is the maximum number of extends edges a chain may traverse,
// per the spec ("single inheritance, maximum depth 3").
const MaxDepth = 3

// Loaded is one template file: the decoded document and, after Validate, its
// item list resolved through the extends chain.
type Loaded struct {
	File     string
	Tpl      model.Template
	Resolved []model.TemplateItem

	// doc is the raw YAML document as generic values, kept for JSON Schema
	// validation so the schema sees exactly what the author wrote rather
	// than what survived struct decoding.
	doc any
}

// Pack is a set of templates sharing one id namespace. Extends references
// resolve across the whole pack, so a user template may extend a bundled one.
type Pack struct {
	ByID map[string]*Loaded
	IDs  []string // load order: sorted by filename within each source
}

// Issue is one validation error or warning, attributed to a file.
type Issue struct {
	File string
	Msg  string
}

func (i Issue) String() string { return i.File + ": " + i.Msg }

// Summary is the per-template information the CLI prints, mirroring
// validate.py's output.
type Summary struct {
	File, ID   string
	Extends    string
	OwnLines   int
	Resolved   int
	Essential  int
	Consumable int

	HasCeiling       bool
	EstimateKg       float64
	CeilingKg        float64
	UnknownWeightIDs []string // non-optional items with no weight_g
}

// Report accumulates everything Validate finds. A pack is loadable iff OK.
type Report struct {
	Errors    []Issue
	Warnings  []Issue
	Summaries []Summary
}

func (r *Report) OK() bool { return len(r.Errors) == 0 }

func (r *Report) errf(file, format string, args ...any) {
	r.Errors = append(r.Errors, Issue{File: file, Msg: fmt.Sprintf(format, args...)})
}

func (r *Report) warnf(file, format string, args ...any) {
	r.Warnings = append(r.Warnings, Issue{File: file, Msg: fmt.Sprintf(format, args...)})
}

// Load parses every *.yaml at the root of each source into one shared
// namespace. Parse-level problems (unreadable file, broken YAML, missing or
// duplicate id) are reported and the file is skipped; everything else waits
// for Validate.
func Load(sources ...fs.FS) (*Pack, *Report) {
	p := &Pack{ByID: map[string]*Loaded{}}
	r := &Report{}
	for _, fsys := range sources {
		files, err := fs.Glob(fsys, "*.yaml")
		if err != nil {
			r.errf("(pack)", "listing templates: %v", err)
			continue
		}
		sort.Strings(files)
		for _, name := range files {
			b, err := fs.ReadFile(fsys, name)
			if err != nil {
				r.errf(name, "reading: %v", err)
				continue
			}
			var doc any
			if err := yaml.Unmarshal(b, &doc); err != nil {
				r.errf(name, "invalid YAML: %v", err)
				continue
			}
			var tpl model.Template
			if err := yaml.Unmarshal(b, &tpl); err != nil {
				r.errf(name, "decoding: %v", err)
				continue
			}
			if tpl.ID == "" {
				r.errf(name, "missing required 'id'")
				continue
			}
			if prev, dup := p.ByID[tpl.ID]; dup {
				r.errf(name, "duplicate template id %q (also declared in %s)", tpl.ID, prev.File)
				continue
			}
			p.ByID[tpl.ID] = &Loaded{File: name, Tpl: tpl, doc: doc}
			p.IDs = append(p.IDs, tpl.ID)
		}
	}
	return p, r
}

// Validate runs schema validation, extends resolution, and the cross-field
// checks over the whole pack, populating each Loaded's Resolved items and the
// report's summaries as it goes.
func Validate(p *Pack, r *Report) {
	for _, id := range p.IDs {
		schemaCheck(p.ByID[id], r)
	}
	for _, id := range p.IDs {
		l := p.ByID[id]
		l.Resolved = resolveItems(p, r, id, 0)
		crossFieldChecks(l, r)
		r.Summaries = append(r.Summaries, summarize(l))
	}
}

// resolveItems ports validate.py's resolve() exactly, including its merge
// order: the parent's items in parent order with overrides replaced in
// place, removals (quantity 0) dropped, and child additions appended in
// child order. Errors leave the template resolving to its own items so
// later checks still run.
func resolveItems(p *Pack, r *Report, id string, depth int) []model.TemplateItem {
	l := p.ByID[id]
	own := append([]model.TemplateItem(nil), l.Tpl.Items...)
	parentID := l.Tpl.Extends
	if parentID == "" {
		return own
	}
	if depth >= MaxDepth {
		r.errf(l.File, "extends chain exceeds max depth %d", MaxDepth)
		return own
	}
	if _, ok := p.ByID[parentID]; !ok {
		r.errf(l.File, "extends unknown template %q", parentID)
		return own
	}

	merged := resolveItems(p, r, parentID, depth+1)
	index := make(map[string]int, len(merged))
	for i, it := range merged {
		index[it.ID] = i
	}
	for _, it := range l.Tpl.Items {
		switch idx, present := index[it.ID]; {
		case it.Quantity == 0:
			if !present {
				r.warnf(l.File, "item %q has quantity 0 (removal) but is not present in parent %q", it.ID, parentID)
				continue
			}
			merged = append(merged[:idx], merged[idx+1:]...)
			delete(index, it.ID)
			for iid, j := range index {
				if j > idx {
					index[iid] = j - 1
				}
			}
		case present:
			merged[idx] = it
		default:
			index[it.ID] = len(merged)
			merged = append(merged, it)
		}
	}
	return merged
}

func summarize(l *Loaded) Summary {
	s := Summary{
		File:     l.File,
		ID:       l.Tpl.ID,
		Extends:  l.Tpl.Extends,
		OwnLines: len(l.Tpl.Items),
		Resolved: len(l.Resolved),
	}
	for _, it := range l.Resolved {
		if it.Crit == model.CritEssential {
			s.Essential++
		}
		if it.Consumable != nil {
			s.Consumable++
		}
	}
	if c := l.Tpl.Container; c != nil && c.TargetWeightKg > 0 {
		s.HasCeiling = true
		s.CeilingKg = c.TargetWeightKg
		s.EstimateKg, s.UnknownWeightIDs = estimateWeight(l.Resolved)
	}
	return s
}

// estimateWeight ports validate.py's pack-weight estimate: only
// fixed-scaling lines are meaningful without a household profile, and only
// countable units (ea, pair, set) multiply by quantity. Non-optional items
// with no declared weight make the estimate a floor, and are returned so the
// report can say so.
func estimateWeight(items []model.TemplateItem) (kg float64, unknown []string) {
	totalG := 0.0
	for _, it := range items {
		if it.WeightG == nil {
			if it.EffectiveCriticality() != model.CritOptional {
				unknown = append(unknown, it.ID)
			}
			continue
		}
		w := *it.WeightG
		if it.EffectiveScaling() == model.ScaleFixed &&
			(it.Unit == model.UnitEa || it.Unit == model.UnitPair || it.Unit == model.UnitSet) {
			totalG += w * it.Quantity
		} else {
			totalG += w
		}
	}
	return totalG / 1000, unknown
}
