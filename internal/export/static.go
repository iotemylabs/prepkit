package export

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
)

// Static writes the offline HTML export into dir: a self-contained set of
// plain pages for a 6-inch greyscale e-ink reader or a low-power offline
// node. No JavaScript, no external requests of any kind, inline document
// styles only, semantic markup, large high-contrast type. The pages are
// structurally incapable of phoning anywhere.
//
// Structure per the build brief: index -> kit list -> per-kit manifest ->
// contact sheet -> plans, every page within two taps of the index (the
// index links each kit and document directly, since e-ink navigation is
// painful).
//
// Static always applies the default sensitivity filter itself — there is no
// sensitive option on this path. Serial numbers, costs, needs, and location
// notes never appear; documents appear only with their own export_ok, and
// never-export kinds not at all. The one document body classification the
// filter keeps (plan and contact-sheet bodies) is exactly what an offline
// reference exists to carry.
//
// dir must not already contain files: the export refuses rather than mixing
// generations of pages.
func Static(dir string, inv model.Inventory, now time.Time) error {
	filtered := Filter(inv)
	today := now.UTC().Truncate(24 * time.Hour)

	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("directory %s is not empty; refusing to mix export generations", dir)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	v := buildStaticView(filtered, today, now)

	pages := map[string]struct {
		title string
		body  *template.Template
		data  any
	}{
		"index.html": {"PrepKit — offline reference", indexBody, v},
		"kits.html":  {"Kits", kitsBody, v},
	}
	for _, kv := range v.Kits {
		pages[kv.File] = struct {
			title string
			body  *template.Template
			data  any
		}{kv.Kit.Name, kitBody, kv}
	}
	if len(v.Contacts) > 0 {
		pages["contacts.html"] = struct {
			title string
			body  *template.Template
			data  any
		}{"Contact sheet", contactsBody, v}
	}
	for _, dv := range v.Plans {
		pages[dv.File] = struct {
			title string
			body  *template.Template
			data  any
		}{dv.Title, docBody, dv}
	}

	for name, p := range pages {
		var body bytes.Buffer
		if err := p.body.Execute(&body, p.data); err != nil {
			return fmt.Errorf("rendering %s: %w", name, err)
		}
		var page bytes.Buffer
		err := layoutTmpl.Execute(&page, layoutData{
			Title:     p.title,
			IsIndex:   name == "index.html",
			Body:      template.HTML(body.String()),
			Generated: v.Generated,
		})
		if err != nil {
			return fmt.Errorf("rendering %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), page.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// --- view models --------------------------------------------------------

type staticView struct {
	Generated string
	Kits      []kitView
	Contacts  []docView
	Plans     []docView
}

type kitView struct {
	Kit           model.Kit
	File          string
	ContainerName string
	PersonLabel   string
	LastVerified  string // "never" when unset
	ItemCount     int
	Categories    []categoryView
}

type categoryView struct {
	Label string
	Items []itemView
}

type itemView struct {
	Name          string
	Product       string
	Quantity      string // formatted effective quantity + unit
	ContainerName string
	Lots          []lotView
}

type lotView struct {
	Quantity string
	Expires  string
	Basis    string
	Status   string
	Expired  bool
}

type docView struct {
	File     string
	Title    string
	Kind     string
	Reviewed string
	Body     string
}

func buildStaticView(inv model.Inventory, today, now time.Time) staticView {
	v := staticView{Generated: now.UTC().Format("2006-01-02 15:04 UTC")}

	containerName := map[string]string{}
	for _, c := range inv.Containers {
		containerName[c.ID] = c.Name
	}
	personLabel := map[string]string{}
	if inv.Household != nil {
		for _, p := range inv.Household.Persons {
			personLabel[p.ID] = p.Label
		}
	}
	lotsByItem := map[string][]model.Lot{}
	for _, l := range inv.Lots {
		if l.EffectiveStatus().CountsTowardStock() {
			lotsByItem[l.ItemID] = append(lotsByItem[l.ItemID], l)
		}
	}
	itemsByKit := map[string][]model.Item{}
	for _, it := range inv.Items {
		if !it.Archived && it.KitID != "" {
			itemsByKit[it.KitID] = append(itemsByKit[it.KitID], it)
		}
	}

	for _, kit := range inv.Kits {
		kv := kitView{
			Kit:           kit,
			File:          "kit-" + kit.ID + ".html",
			ContainerName: containerName[kit.ContainerID],
			PersonLabel:   personLabel[kit.AssignedPersonID],
			LastVerified:  kit.LastVerified,
		}
		if kv.LastVerified == "" {
			kv.LastVerified = "never"
		}
		byCat := map[model.Category][]itemView{}
		for _, it := range itemsByKit[kit.ID] {
			iv := itemView{
				Name:          it.Name,
				Product:       it.Product,
				ContainerName: containerName[it.ContainerID],
			}
			lots := lotsByItem[it.ID]
			qty := 0.0
			if len(lots) == 0 {
				if it.Quantity != nil {
					qty = *it.Quantity
				}
			}
			for _, l := range lots {
				qty += l.Quantity
				lv := lotView{
					Quantity: formatQty(l.Quantity, l.Unit),
					Expires:  l.Expires,
					Basis:    expiryBasisLabel(l.ExpiryBasis),
					Status:   string(l.EffectiveStatus()),
				}
				if isExpiredDate(l.Expires, today) {
					lv.Expired = true
				}
				iv.Lots = append(iv.Lots, lv)
			}
			iv.Quantity = formatQty(qty, it.Unit)
			byCat[it.Category] = append(byCat[it.Category], iv)
			kv.ItemCount++
		}
		for _, cat := range model.Categories {
			if items, ok := byCat[cat]; ok {
				kv.Categories = append(kv.Categories, categoryView{Label: categoryLabel(cat), Items: items})
			}
		}
		v.Kits = append(v.Kits, kv)
	}

	for _, d := range inv.Documents {
		dv := docView{
			File:     "doc-" + d.ID + ".html",
			Title:    d.Title,
			Kind:     string(d.Kind),
			Reviewed: d.LastReviewed,
			Body:     d.Body,
		}
		if d.Kind == model.DocContactSheet {
			v.Contacts = append(v.Contacts, dv)
		} else {
			v.Plans = append(v.Plans, dv)
		}
	}
	return v
}

func formatQty(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if unit != "" {
		s += " " + unit
	}
	return s
}

func isExpiredDate(expires string, today time.Time) bool {
	if expires == "" {
		return false
	}
	d, err := model.ParseDate(expires)
	return err == nil && d.Before(today)
}

func expiryBasisLabel(b model.ExpiryBasis) string {
	switch b {
	case model.ExpiryPrintedBestBy:
		return "best by"
	case model.ExpiryPrintedUseBy:
		return "use by"
	case model.ExpiryPrintedExp:
		return "exp"
	case model.ExpiryEstimated:
		return "estimated"
	case model.ExpiryMfgPlusShelf:
		return "estimated from shelf life"
	default:
		return ""
	}
}

func categoryLabel(c model.Category) string {
	switch c {
	case model.CatFirstAid:
		return "First aid"
	default:
		s := string(c)
		return string(s[0]-'a'+'A') + s[1:]
	}
}

// --- templates ----------------------------------------------------------
//
// One document <style> block per page and nothing else: no external CSS, no
// fonts, no images, no scripts. Sized for a 6-inch 800x600 greyscale e-ink
// display: large serif type, high contrast, tables that fit the width.

type layoutData struct {
	Title     string
	IsIndex   bool
	Body      template.HTML
	Generated string
}

var layoutTmpl = template.Must(template.New("layout").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body { font-family: Georgia, "Times New Roman", serif; font-size: 20px; line-height: 1.45; color: #000; background: #fff; margin: 16px; }
h1 { font-size: 1.5em; margin: 0.4em 0; }
h2 { font-size: 1.2em; margin: 0.8em 0 0.3em; border-bottom: 2px solid #000; }
nav { margin-bottom: 0.8em; font-size: 0.9em; }
a { color: #000; }
table { border-collapse: collapse; width: 100%; margin: 0.4em 0; }
th, td { border: 1px solid #000; padding: 6px 8px; text-align: left; vertical-align: top; }
th { background: #ddd; }
ul { padding-left: 1.2em; }
li { margin: 0.25em 0; }
pre { white-space: pre-wrap; font-family: inherit; border: 1px solid #000; padding: 10px; }
footer { margin-top: 1.5em; border-top: 1px solid #000; padding-top: 0.5em; font-size: 0.8em; }
.meta { font-size: 0.85em; }
.expired { font-weight: bold; }
</style>
</head>
<body>
{{if not .IsIndex}}<nav><a href="index.html">&#8592; Index</a></nav>{{end}}
<main>
{{.Body}}
</main>
<footer>
<p>Generated {{.Generated}} by PrepKit. This is organizational tooling, not emergency
guidance; follow official instructions during an actual emergency.</p>
</footer>
</body>
</html>
`))

var indexBody = template.Must(template.New("index").Parse(`<h1>PrepKit offline reference</h1>
<h2>Kits</h2>
<ul>
<li><a href="kits.html">All kits</a></li>
{{range .Kits}}<li><a href="{{.File}}">{{.Kit.Name}}</a> ({{.ItemCount}} items)</li>
{{end}}</ul>
{{if .Contacts}}<h2>Contact sheet</h2>
<ul><li><a href="contacts.html">Emergency contacts</a></li></ul>
{{end}}{{if .Plans}}<h2>Plans and documents</h2>
<ul>
{{range .Plans}}<li><a href="{{.File}}">{{.Title}}</a></li>
{{end}}</ul>
{{end}}`))

var kitsBody = template.Must(template.New("kits").Parse(`<h1>Kits</h1>
<table>
<tr><th>Kit</th><th>Layer</th><th>Duration</th><th>Items</th><th>Last verified</th></tr>
{{range .Kits}}<tr><td><a href="{{.File}}">{{.Kit.Name}}</a></td><td>{{.Kit.Layer}}</td><td>{{.Kit.Duration}}</td><td>{{.ItemCount}}</td><td>{{.LastVerified}}</td></tr>
{{end}}</table>`))

var kitBody = template.Must(template.New("kit").Parse(`<h1>{{.Kit.Name}}</h1>
<p class="meta">
{{if .Kit.Layer}}Layer: {{.Kit.Layer}}.{{end}}
{{if .Kit.Duration}}Duration: {{.Kit.Duration}}.{{end}}
{{if .ContainerName}}Stored in: {{.ContainerName}}.{{end}}
{{if .PersonLabel}}Assigned to: {{.PersonLabel}}.{{end}}
Last physically verified: {{.LastVerified}}.
</p>
{{if not .Categories}}<p>This kit has no items recorded.</p>{{end}}
{{range .Categories}}<h2>{{.Label}}</h2>
<table>
<tr><th>Item</th><th>Quantity</th><th>Where</th><th>Dated lots</th></tr>
{{range .Items}}<tr>
<td>{{.Name}}{{if .Product}}<br><span class="meta">{{.Product}}</span>{{end}}</td>
<td>{{.Quantity}}</td>
<td>{{.ContainerName}}</td>
<td>{{range .Lots}}{{.Quantity}}{{if .Expires}} &mdash; {{if .Basis}}{{.Basis}} {{end}}{{.Expires}}{{end}}{{if .Expired}} <span class="expired">EXPIRED</span>{{end}}<br>{{end}}</td>
</tr>
{{end}}</table>
{{end}}`))

var contactsBody = template.Must(template.New("contacts").Parse(`<h1>Emergency contacts</h1>
{{range .Contacts}}<h2>{{.Title}}</h2>
{{if .Reviewed}}<p class="meta">Last reviewed {{.Reviewed}}.</p>{{end}}
<pre>{{.Body}}</pre>
{{end}}`))

var docBody = template.Must(template.New("doc").Parse(`<h1>{{.Title}}</h1>
<p class="meta">{{.Kind}}{{if .Reviewed}} &mdash; last reviewed {{.Reviewed}}{{end}}</p>
<pre>{{.Body}}</pre>`))
