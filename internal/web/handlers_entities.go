package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/internal/readiness"
)

// --- household and persons ----------------------------------------------

func (s *Server) householdPage(w http.ResponseWriter, r *http.Request) {
	h, err := s.st.GetHousehold(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "household.html", map[string]any{
		"Household":    h,
		"AgeBands":     model.AgeBands,
		"Climates":     householdClimates(),
		"Environments": householdEnvironments(),
	})
}

// householdClimates and householdEnvironments exclude "any": a template may
// assume any climate, but a household lives in one.
func householdClimates() []model.Climate {
	var out []model.Climate
	for _, c := range model.Climates {
		if c != model.ClimateAny {
			out = append(out, c)
		}
	}
	return out
}

func householdEnvironments() []model.Environment {
	var out []model.Environment
	for _, e := range model.Environments {
		if e != model.EnvAny {
			out = append(out, e)
		}
	}
	return out
}

func (s *Server) householdSave(w http.ResponseWriter, r *http.Request) {
	pets, err := strconv.Atoi(strings.TrimSpace(r.FormValue("pets")))
	if r.FormValue("pets") == "" {
		pets, err = 0, nil
	}
	if err != nil {
		s.fail(w, errValidationf("pets must be a whole number"))
		return
	}
	h := &model.Household{
		Label:       strings.TrimSpace(r.FormValue("label")),
		Pets:        pets,
		Climate:     model.Climate(r.FormValue("climate")),
		Environment: model.Environment(r.FormValue("environment")),
	}
	if err := s.st.UpdateHousehold(r.Context(), h); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/household", http.StatusSeeOther)
}

func personFromForm(r *http.Request) *model.Person {
	var needs []string
	for _, line := range strings.Split(r.FormValue("needs"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			needs = append(needs, line)
		}
	}
	return &model.Person{
		Label:   strings.TrimSpace(r.FormValue("label")),
		AgeBand: model.AgeBand(r.FormValue("age_band")),
		Needs:   needs,
	}
}

func (s *Server) personCreate(w http.ResponseWriter, r *http.Request) {
	if err := s.st.CreatePerson(r.Context(), personFromForm(r)); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/household", http.StatusSeeOther)
}

func (s *Server) personUpdate(w http.ResponseWriter, r *http.Request) {
	p := personFromForm(r)
	p.ID = r.PathValue("id")
	if err := s.st.UpdatePerson(r.Context(), p); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/household", http.StatusSeeOther)
}

func (s *Server) personDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeletePerson(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/household", http.StatusSeeOther)
}

// --- containers ---------------------------------------------------------

func (s *Server) containersPage(w http.ResponseWriter, r *http.Request) {
	cs, err := s.st.ListContainers(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	name := map[string]string{}
	for _, c := range cs {
		name[c.ID] = c.Name
	}
	type row struct {
		C          model.Container
		ParentName string
	}
	var rows []row
	for _, c := range cs {
		rows = append(rows, row{C: c, ParentName: name[c.ParentID]})
	}
	s.render(w, http.StatusOK, "containers.html", map[string]any{
		"Rows":       rows,
		"Containers": cs,
		"Types":      model.ContainerTypes,
	})
}

func containerFromForm(r *http.Request) (*model.Container, error) {
	weight, err := formPtrFloat(r, "weight_g")
	if err != nil {
		return nil, err
	}
	return &model.Container{
		Name:         strings.TrimSpace(r.FormValue("name")),
		ParentID:     r.FormValue("parent_id"),
		Type:         model.ContainerType(r.FormValue("type")),
		ColorCode:    strings.TrimSpace(r.FormValue("color_code")),
		LocationNote: strings.TrimSpace(r.FormValue("location_note")),
		WeightG:      weight,
	}, nil
}

func (s *Server) containerCreate(w http.ResponseWriter, r *http.Request) {
	c, err := containerFromForm(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.CreateContainer(r.Context(), c); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/containers/"+c.ID, http.StatusSeeOther)
}

func (s *Server) containerPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := s.st.GetContainer(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	all, err := s.st.ListContainers(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	var children, parents []model.Container
	for _, other := range all {
		if other.ParentID == c.ID {
			children = append(children, other)
		}
		if other.ID != c.ID {
			parents = append(parents, other)
		}
	}
	items, err := s.st.ListItems(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	var contents []model.Item
	for _, it := range items {
		if it.ContainerID == c.ID && !it.Archived {
			contents = append(contents, it)
		}
	}
	s.render(w, http.StatusOK, "container.html", map[string]any{
		"C":        c,
		"Children": children,
		"Parents":  parents,
		"Contents": contents,
		"Types":    model.ContainerTypes,
	})
}

func (s *Server) containerUpdate(w http.ResponseWriter, r *http.Request) {
	c, err := containerFromForm(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	c.ID = r.PathValue("id")
	// Keep the existing slug: reprinting every label because a name changed
	// would defeat the point of opaque tokens.
	old, err := s.st.GetContainer(r.Context(), c.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	c.QRSlug = old.QRSlug
	if err := s.st.UpdateContainer(r.Context(), c); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/containers/"+c.ID, http.StatusSeeOther)
}

func (s *Server) containerDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteContainer(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/containers", http.StatusSeeOther)
}

// --- kits ---------------------------------------------------------------

func (s *Server) kitsPage(w http.ResponseWriter, r *http.Request) {
	kits, err := s.st.ListKits(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "kits.html", map[string]any{"Kits": kits})
}

func (s *Server) kitNewForm(w http.ResponseWriter, r *http.Request) {
	containers, err := s.st.ListContainers(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	persons, err := s.st.ListPersons(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	type tplOpt struct{ ID, Name string }
	var tpls []tplOpt
	for _, id := range s.pack.IDs {
		tpls = append(tpls, tplOpt{ID: id, Name: s.pack.ByID[id].Tpl.Name})
	}
	s.render(w, http.StatusOK, "kit_new.html", map[string]any{
		"Templates":  tpls,
		"Containers": containers,
		"Persons":    persons,
		"Layers":     model.Layers,
		"Durations":  model.Durations,
	})
}

// kitCreate creates a blank kit, or instantiates a template: the kit takes
// the template's layer/duration/tier, and every resolved line pre-creates an
// item stub linked by template_item_id — the linkage the whole model
// depends on, made automatically instead of by hand. Consumable lines get
// no quantity (stock arrives as dated lots); non-consumables start at 0.
func (s *Server) kitCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kit := &model.Kit{
		Name:             strings.TrimSpace(r.FormValue("name")),
		ContainerID:      r.FormValue("container_id"),
		AssignedPersonID: r.FormValue("assigned_person_id"),
	}

	tplID := r.FormValue("template_id")
	if tplID == "" {
		kit.Layer = model.Layer(r.FormValue("layer"))
		kit.Duration = model.Duration(r.FormValue("duration"))
		if err := s.st.CreateKit(ctx, kit); err != nil {
			s.fail(w, err)
			return
		}
		http.Redirect(w, r, "/kits/"+kit.ID, http.StatusSeeOther)
		return
	}

	loaded, ok := s.pack.ByID[tplID]
	if !ok {
		s.fail(w, errValidationf("unknown template %q", tplID))
		return
	}
	if kit.Name == "" {
		kit.Name = loaded.Tpl.Name
	}
	kit.TemplateID = tplID
	kit.Layer = loaded.Tpl.Layer
	kit.Duration = loaded.Tpl.Duration
	kit.EvacuationTier = loaded.Tpl.EvacuationTier
	if err := s.st.CreateKit(ctx, kit); err != nil {
		s.fail(w, err)
		return
	}
	for _, line := range loaded.Resolved {
		it := &model.Item{
			Name:           line.Name,
			Category:       line.Category,
			TemplateItemID: line.ID,
			KitID:          kit.ID,
			ContainerID:    kit.ContainerID,
			Unit:           string(line.Unit),
		}
		if line.Consumable == nil {
			zero := 0.0
			it.Quantity = &zero
		}
		if err := s.st.CreateItem(ctx, it); err != nil {
			s.fail(w, err)
			return
		}
	}
	http.Redirect(w, r, "/kits/"+kit.ID, http.StatusSeeOther)
}

func (s *Server) kitPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kit, err := s.st.GetKit(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	items, err := s.st.ListItemsByKit(ctx, kit.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	lots, err := s.st.ListLots(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	lotCount := map[string]int{}
	lotQty := map[string]float64{}
	for _, l := range lots {
		if l.EffectiveStatus().CountsTowardStock() {
			lotCount[l.ItemID]++
			lotQty[l.ItemID] += l.Quantity
		}
	}
	type row struct {
		It   model.Item
		Qty  string
		Lots int
	}
	var rows []row
	// Sort by category order, then name, so the page reads like a manifest.
	for _, cat := range model.Categories {
		for _, it := range items {
			if it.Category != cat || it.Archived {
				continue
			}
			q := 0.0
			if lotCount[it.ID] > 0 {
				q = lotQty[it.ID]
			} else if it.Quantity != nil {
				q = *it.Quantity
			}
			rows = append(rows, row{It: it, Qty: fmtFloat(q), Lots: lotCount[it.ID]})
		}
	}
	var archived []model.Item
	for _, it := range items {
		if it.Archived {
			archived = append(archived, it)
		}
	}

	containers, err := s.st.ListContainers(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	persons, err := s.st.ListPersons(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	_, hasTemplate := s.pack.ByID[kit.TemplateID]
	s.render(w, http.StatusOK, "kit.html", map[string]any{
		"Kit":         kit,
		"Rows":        rows,
		"Archived":    archived,
		"Containers":  containers,
		"Persons":     persons,
		"Layers":      model.Layers,
		"Durations":   model.Durations,
		"HasTemplate": hasTemplate,
	})
}

func (s *Server) kitUpdate(w http.ResponseWriter, r *http.Request) {
	kit, err := s.st.GetKit(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	kit.Name = strings.TrimSpace(r.FormValue("name"))
	kit.Layer = model.Layer(r.FormValue("layer"))
	kit.Duration = model.Duration(r.FormValue("duration"))
	kit.ContainerID = r.FormValue("container_id")
	kit.AssignedPersonID = r.FormValue("assigned_person_id")
	kit.Notes = r.FormValue("notes")
	if err := s.st.UpdateKit(r.Context(), kit); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/kits/"+kit.ID, http.StatusSeeOther)
}

func (s *Server) kitDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteKit(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/kits", http.StatusSeeOther)
}

// kitVerify records the physical act: last_verified moves to today AND a
// maintenance event is logged, giving both the headline date and the audit
// trail entry that interval-based checks anchor on.
func (s *Server) kitVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kit, err := s.st.GetKit(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	kit.LastVerified = today()
	if err := s.st.UpdateKit(ctx, kit); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.CreateMaintenance(ctx, &model.MaintenanceEvent{
		Date: today(), Kind: model.MaintInspection, KitID: kit.ID,
		Notes: "verified via web UI",
	}); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/kits/"+kit.ID, http.StatusSeeOther)
}

func (s *Server) kitReadiness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kit, err := s.st.GetKit(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	loaded, ok := s.pack.ByID[kit.TemplateID]
	if !ok {
		s.fail(w, errValidationf("kit %q has no loaded template to score against", kit.Name))
		return
	}
	items, err := s.st.ListItemsByKit(ctx, kit.ID)
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
	rep := readiness.Score(readiness.Input{
		Template:  loaded.Tpl,
		Items:     loaded.Resolved,
		Household: household,
		Kit:       *kit,
		UserItems: items,
		Lots:      lots,
		Events:    events,
		Now:       time.Now(),
	}, readiness.Config{})
	s.render(w, http.StatusOK, "kit_readiness.html", map[string]any{
		"Kit":     kit,
		"TplName": loaded.Tpl.Name,
		"R":       rep,
	})
}

// --- items and lots -----------------------------------------------------

type lineOpt struct{ ID, Name string }

func (s *Server) templateLines(kitTemplateID string) []lineOpt {
	loaded, ok := s.pack.ByID[kitTemplateID]
	if !ok {
		return nil
	}
	var out []lineOpt
	for _, l := range loaded.Resolved {
		out = append(out, lineOpt{ID: l.ID, Name: l.Name})
	}
	return out
}

func (s *Server) itemNewForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kits, err := s.st.ListKits(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	containers, err := s.st.ListContainers(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	kitID := r.URL.Query().Get("kit")
	var lines []lineOpt
	for _, k := range kits {
		if k.ID == kitID {
			lines = s.templateLines(k.TemplateID)
		}
	}
	s.render(w, http.StatusOK, "item_new.html", map[string]any{
		"Kits":       kits,
		"Containers": containers,
		"Categories": model.Categories,
		"KitID":      kitID,
		"Lines":      lines,
	})
}

func (s *Server) itemFromForm(r *http.Request, it *model.Item) error {
	qty, err := formPtrFloat(r, "quantity")
	if err != nil {
		return err
	}
	weight, err := formPtrFloat(r, "weight_g")
	if err != nil {
		return err
	}
	cost, err := formPtrFloat(r, "cost")
	if err != nil {
		return err
	}
	it.Name = strings.TrimSpace(r.FormValue("name"))
	it.Category = model.Category(r.FormValue("category"))
	it.TemplateItemID = strings.TrimSpace(r.FormValue("template_item_id"))
	it.KitID = r.FormValue("kit_id")
	it.ContainerID = r.FormValue("container_id")
	it.Quantity = qty
	it.Unit = strings.TrimSpace(r.FormValue("unit"))
	it.Product = strings.TrimSpace(r.FormValue("product"))
	it.WeightG = weight
	it.Acquired = strings.TrimSpace(r.FormValue("acquired"))
	it.Cost = cost
	it.Serial = strings.TrimSpace(r.FormValue("serial"))
	it.Notes = r.FormValue("notes")
	it.Archived = r.FormValue("archived") == "on"
	return nil
}

func (s *Server) itemCreate(w http.ResponseWriter, r *http.Request) {
	var it model.Item
	if err := s.itemFromForm(r, &it); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.CreateItem(r.Context(), &it); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/items/"+it.ID, http.StatusSeeOther)
}

func (s *Server) itemPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	it, err := s.st.GetItem(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	lots, err := s.st.ListLotsByItem(ctx, it.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	kits, err := s.st.ListKits(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	containers, err := s.st.ListContainers(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	var lines []lineOpt
	for _, k := range kits {
		if k.ID == it.KitID {
			lines = s.templateLines(k.TemplateID)
		}
	}
	s.render(w, http.StatusOK, "item.html", map[string]any{
		"It":          it,
		"Lots":        lots,
		"Kits":        kits,
		"Containers":  containers,
		"Categories":  model.Categories,
		"Lines":       lines,
		"Statuses":    model.LotStatuses,
		"ExpiryBases": model.ExpiryBases,
		"HasLots":     len(lots) > 0,
	})
}

func (s *Server) itemUpdate(w http.ResponseWriter, r *http.Request) {
	it, err := s.st.GetItem(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.itemFromForm(r, it); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.UpdateItem(r.Context(), it); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/items/"+it.ID, http.StatusSeeOther)
}

func (s *Server) itemDelete(w http.ResponseWriter, r *http.Request) {
	it, err := s.st.GetItem(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.DeleteItem(r.Context(), it.ID); err != nil {
		s.fail(w, err)
		return
	}
	dest := "/kits"
	if it.KitID != "" {
		dest = "/kits/" + it.KitID
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// itemCheck logs a physical inspection of the item, which resets the
// interval-based check clock in readiness.
func (s *Server) itemCheck(w http.ResponseWriter, r *http.Request) {
	it, err := s.st.GetItem(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.CreateMaintenance(r.Context(), &model.MaintenanceEvent{
		Date: today(), Kind: model.MaintInspection, ItemID: it.ID, KitID: it.KitID,
		Notes: "checked via web UI",
	}); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/items/"+it.ID, http.StatusSeeOther)
}

func lotFromForm(r *http.Request, l *model.Lot) error {
	qty, err := formFloat(r, "quantity")
	if err != nil {
		return err
	}
	l.Quantity = qty
	l.Unit = strings.TrimSpace(r.FormValue("unit"))
	l.Expires = strings.TrimSpace(r.FormValue("expires"))
	l.ExpiryBasis = model.ExpiryBasis(r.FormValue("expiry_basis"))
	l.Opened = strings.TrimSpace(r.FormValue("opened"))
	l.NextCheck = strings.TrimSpace(r.FormValue("next_check"))
	l.Status = model.LotStatus(r.FormValue("status"))
	l.Notes = r.FormValue("notes")
	return nil
}

func (s *Server) lotCreate(w http.ResponseWriter, r *http.Request) {
	l := model.Lot{ItemID: r.PathValue("id")}
	if err := lotFromForm(r, &l); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.CreateLot(r.Context(), &l); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/items/"+l.ItemID, http.StatusSeeOther)
}

func (s *Server) lotUpdate(w http.ResponseWriter, r *http.Request) {
	l, err := s.st.GetLot(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := lotFromForm(r, l); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.UpdateLot(r.Context(), l); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/items/"+l.ItemID, http.StatusSeeOther)
}

func (s *Server) lotDelete(w http.ResponseWriter, r *http.Request) {
	l, err := s.st.GetLot(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.DeleteLot(r.Context(), l.ID); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/items/"+l.ItemID, http.StatusSeeOther)
}

// --- documents ----------------------------------------------------------

func (s *Server) documentsPage(w http.ResponseWriter, r *http.Request) {
	docs, err := s.st.ListDocuments(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	type row struct {
		D           model.Document
		NeverExport bool
	}
	var rows []row
	for _, d := range docs {
		rows = append(rows, row{D: d, NeverExport: d.Kind.NeverExportUnencrypted()})
	}
	s.render(w, http.StatusOK, "documents.html", map[string]any{
		"Rows":  rows,
		"Kinds": model.DocumentKinds,
	})
}

func documentFromForm(r *http.Request, d *model.Document) error {
	interval, err := formFloat(r, "review_interval_months")
	if err != nil {
		return err
	}
	d.Title = strings.TrimSpace(r.FormValue("title"))
	d.Kind = model.DocumentKind(r.FormValue("kind"))
	d.Body = r.FormValue("body")
	d.ReviewIntervalMonths = interval
	d.LastReviewed = strings.TrimSpace(r.FormValue("last_reviewed"))
	d.ExportOK = r.FormValue("export_ok") == "on"
	return nil
}

func (s *Server) documentCreate(w http.ResponseWriter, r *http.Request) {
	var d model.Document
	if err := documentFromForm(r, &d); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.CreateDocument(r.Context(), &d); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/documents/"+d.ID, http.StatusSeeOther)
}

func (s *Server) documentPage(w http.ResponseWriter, r *http.Request) {
	d, err := s.st.GetDocument(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, http.StatusOK, "document.html", map[string]any{
		"D":           d,
		"Kinds":       model.DocumentKinds,
		"NeverExport": d.Kind.NeverExportUnencrypted(),
	})
}

func (s *Server) documentUpdate(w http.ResponseWriter, r *http.Request) {
	d, err := s.st.GetDocument(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := documentFromForm(r, d); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.st.UpdateDocument(r.Context(), d); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/documents/"+d.ID, http.StatusSeeOther)
}

func (s *Server) documentDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteDocument(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/documents", http.StatusSeeOther)
}
