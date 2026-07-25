package web

import (
	"archive/zip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/iotemylabs/prepkit/internal/export"
	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/internal/readiness"

	qrcode "github.com/skip2/go-qrcode"
)

// --- dashboard ----------------------------------------------------------

type lotRow struct {
	ItemID    string
	ItemName  string
	KitName   string
	Quantity  float64
	Unit      string
	Expires   string
	DaysUntil int
}

type checkRow struct {
	ItemID   string
	ItemName string
	KitName  string
	Due      string
	Reason   string
}

type kitSummary struct {
	Kit    model.Kit
	Report *readiness.Report // nil when the kit has no loaded template
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kits, err := s.st.ListKits(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	items, err := s.st.ListItems(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	lots, err := s.st.ListLots(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	events, err := s.st.ListMaintenance(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	household, err := s.st.GetHousehold(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}

	itemByID := map[string]model.Item{}
	for _, it := range items {
		itemByID[it.ID] = it
	}
	kitName := map[string]string{}
	for _, k := range kits {
		kitName[k.ID] = k.Name
	}
	nameFor := func(itemID string) (item, kit string) {
		it := itemByID[itemID]
		return it.Name, kitName[it.KitID]
	}

	now := time.Now()
	todayT := now.UTC().Truncate(24 * time.Hour)
	var expired, expiring []lotRow
	var overdue []checkRow

	// Date-driven flags across the whole inventory, kit or no kit.
	for _, lot := range lots {
		it, known := itemByID[lot.ItemID]
		if !known || it.Archived || !lot.EffectiveStatus().CountsTowardStock() {
			continue
		}
		iName, kName := nameFor(lot.ItemID)
		if lot.Expires != "" {
			if d, err := model.ParseDate(lot.Expires); err == nil {
				days := int(d.Sub(todayT).Hours() / 24)
				row := lotRow{ItemID: lot.ItemID, ItemName: iName, KitName: kName,
					Quantity: lot.Quantity, Unit: lot.Unit, Expires: lot.Expires, DaysUntil: days}
				switch {
				case d.Before(todayT):
					expired = append(expired, row)
				case days <= 90:
					expiring = append(expiring, row)
				}
			}
		}
		if lot.NextCheck != "" {
			if d, err := model.ParseDate(lot.NextCheck); err == nil && d.Before(todayT) {
				overdue = append(overdue, checkRow{ItemID: lot.ItemID, ItemName: iName,
					KitName: kName, Due: lot.NextCheck, Reason: "next_check date has passed"})
			}
		}
	}

	// Interval-driven flags come from the readiness engine, per kit with a
	// loaded template; lot-based flags are already collected above, so only
	// the interval entries (no LotID) are taken.
	var summaries []kitSummary
	for _, kit := range kits {
		sum := kitSummary{Kit: kit}
		if loaded, ok := s.pack.ByID[kit.TemplateID]; ok {
			rep := readiness.Score(readiness.Input{
				Template:  loaded.Tpl,
				Items:     loaded.Resolved,
				Household: household,
				Kit:       kit,
				UserItems: filterByKit(items, kit.ID),
				Lots:      lots,
				Events:    events,
				Now:       now,
			}, readiness.Config{})
			sum.Report = rep
			for _, c := range rep.Freshness.CheckOverdue {
				if c.LotID == "" {
					overdue = append(overdue, checkRow{ItemID: c.ItemID, ItemName: c.ItemName,
						KitName: kit.Name, Due: c.Due, Reason: c.Reason})
				}
			}
		}
		summaries = append(summaries, sum)
	}

	sort.Slice(expired, func(i, j int) bool { return expired[i].Expires < expired[j].Expires })
	sort.Slice(expiring, func(i, j int) bool { return expiring[i].Expires < expiring[j].Expires })
	sort.Slice(overdue, func(i, j int) bool { return overdue[i].ItemName < overdue[j].ItemName })

	s.render(w, http.StatusOK, "dashboard.html", map[string]any{
		"Expired":  expired,
		"Expiring": expiring,
		"Overdue":  overdue,
		"Kits":     summaries,
	})
}

func filterByKit(items []model.Item, kitID string) []model.Item {
	var out []model.Item
	for _, it := range items {
		if it.KitID == kitID {
			out = append(out, it)
		}
	}
	return out
}

// --- template browser ---------------------------------------------------

func (s *Server) templatesPage(w http.ResponseWriter, r *http.Request) {
	type row struct {
		ID, Name  string
		Layer     model.Layer
		Duration  model.Duration
		Extends   string
		Lines     int
		Essential int
	}
	var rows []row
	for _, id := range s.pack.IDs {
		l := s.pack.ByID[id]
		rw := row{ID: id, Name: l.Tpl.Name, Layer: l.Tpl.Layer, Duration: l.Tpl.Duration,
			Extends: l.Tpl.Extends, Lines: len(l.Resolved)}
		for _, it := range l.Resolved {
			if it.EffectiveCriticality() == model.CritEssential {
				rw.Essential++
			}
		}
		rows = append(rows, rw)
	}
	s.render(w, http.StatusOK, "templates_list.html", map[string]any{"Rows": rows})
}

func (s *Server) templatePage(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pack.ByID[r.PathValue("id")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.render(w, http.StatusOK, "template_view.html", map[string]any{
		"Tpl":   l.Tpl,
		"Lines": l.Resolved,
	})
}

// --- exports ------------------------------------------------------------

func (s *Server) exportsPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "exports.html", nil)
}

func (s *Server) exportJSON(w http.ResponseWriter, r *http.Request) {
	s.serveJSONExport(w, r, export.Options{}, "prepkit-inventory.json")
}

// exportJSONFull is the local-backup form: it restores the default-stripped
// sensitive fields. Documents of never-export kinds are still absent — no
// route reaches them.
func (s *Server) exportJSONFull(w http.ResponseWriter, r *http.Request) {
	s.serveJSONExport(w, r, export.Options{IncludeSensitive: true}, "prepkit-inventory-full.json")
}

func (s *Server) serveJSONExport(w http.ResponseWriter, r *http.Request, opts export.Options, filename string) {
	inv, err := s.st.Snapshot(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	b, err := export.JSON(*inv, time.Now(), opts)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Write(b)
}

func (s *Server) exportOfflineZip(w http.ResponseWriter, r *http.Request) {
	inv, err := s.st.Snapshot(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	dir, err := os.MkdirTemp("", "prepkit-static-")
	if err != nil {
		s.fail(w, err)
		return
	}
	defer os.RemoveAll(dir)
	site := filepath.Join(dir, "prepkit-offline")
	if err := export.Static(site, *inv, time.Now()); err != nil {
		s.fail(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="prepkit-offline.zip"`)
	zw := zip.NewWriter(w)
	err = filepath.WalkDir(site, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		f, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(f, src)
		return err
	})
	if err != nil {
		s.log.Error("offline zip", "err", err.Error())
	}
	zw.Close()
}

// --- QR -----------------------------------------------------------------

// containerQRPNG encodes the container's opaque slug — the slug alone,
// never a URL: a label on a bin in a garage is a public object, and the
// token is only meaningful to the local instance that issued it.
func (s *Server) containerQRPNG(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	png, err := qrcode.Encode(c.QRSlug, qrcode.Medium, 512)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

func (s *Server) containerLabel(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "label.html", map[string]any{"Container": c})
}

// qrResolve turns a scanned or typed slug back into its container page.
func (s *Server) qrResolve(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.GetContainerByQRSlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/containers/"+c.ID, http.StatusSeeOther)
}
