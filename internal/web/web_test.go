package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/internal/store"
	tpack "github.com/iotemylabs/prepkit/internal/template"
	"github.com/iotemylabs/prepkit/templates"
)

const testSecret = "correct horse battery staple"

var ctx = context.Background()

type env struct {
	st     *store.Store
	ts     *httptest.Server
	client *http.Client
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	pack, rep := tpack.Load(templates.FS)
	tpack.Validate(pack, rep)
	if !rep.OK() {
		t.Fatalf("bundled templates invalid: %v", rep.Errors)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(New(st, pack, testSecret, logger).Handler())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	return &env{st: st, ts: ts, client: client}
}

func (e *env) login(t *testing.T) {
	t.Helper()
	resp, err := e.client.PostForm(e.ts.URL+"/login", url.Values{"secret": {testSecret}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/" {
		t.Fatalf("login did not land on dashboard: %s", resp.Request.URL)
	}
}

func (e *env) get(t *testing.T, path string) (int, string) {
	t.Helper()
	resp, err := e.client.Get(e.ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (e *env) post(t *testing.T, path string, form url.Values) *http.Response {
	t.Helper()
	resp, err := e.client.PostForm(e.ts.URL+path, form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestAuthRequired(t *testing.T) {
	e := newEnv(t)

	// Unauthenticated GETs bounce to the login page.
	code, body := e.get(t, "/kits")
	if code != http.StatusOK || !strings.Contains(body, "Admin secret") {
		t.Errorf("unauthenticated /kits: code %d, expected redirect to login form", code)
	}

	// Unauthenticated POSTs are refused outright.
	resp, err := http.PostForm(e.ts.URL+"/kits", url.Values{"name": {"x"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated POST: %d, want 401", resp.StatusCode)
	}

	// The wrong secret does not create a session.
	resp2, err := e.client.PostForm(e.ts.URL+"/login", url.Values{"secret": {"wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d, want 401", resp2.StatusCode)
	}

	e.login(t)
	if code, _ := e.get(t, "/kits"); code != http.StatusOK {
		t.Errorf("authenticated /kits: %d", code)
	}
}

func TestOriginCheck(t *testing.T) {
	e := newEnv(t)
	e.login(t)
	req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/kits", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d, want 403", resp.StatusCode)
	}
}

// TestInstantiateAndReadiness drives the main flow end to end: instantiate
// the vehicle template (the 21->41 fixture), confirm the stub items and
// linkage, and render the readiness report.
func TestInstantiateAndReadiness(t *testing.T) {
	e := newEnv(t)
	e.login(t)

	resp := e.post(t, "/kits", url.Values{
		"template_id": {"vehicle-kit-baseline"},
		"name":        {"My car kit"},
	})
	if !strings.HasPrefix(resp.Request.URL.Path, "/kits/") {
		t.Fatalf("instantiate did not land on kit page: %s", resp.Request.URL)
	}
	kitID := strings.TrimPrefix(resp.Request.URL.Path, "/kits/")

	items, err := e.st.ListItemsByKit(ctx, kitID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 41 {
		t.Fatalf("instantiation created %d items, want 41", len(items))
	}
	linked := map[string]bool{}
	for _, it := range items {
		if it.TemplateItemID == "" {
			t.Errorf("item %q has no template linkage", it.Name)
		}
		linked[it.TemplateItemID] = true
		if it.KitID != kitID {
			t.Errorf("item %q not attached to kit", it.Name)
		}
	}
	if !linked["regional-road-map"] || linked["paper-map"] {
		t.Error("instantiated lines do not match the resolved template (removal/addition wrong)")
	}

	code, body := e.get(t, "/kits/"+kitID+"/readiness")
	if code != http.StatusOK {
		t.Fatalf("readiness page: %d", code)
	}
	for _, want := range []string{
		"INCOMPLETE", // empty kit with essential lines
		"Essential",  // three separate figures...
		"Recommended",
		"Optional",
		"never averaged",         // ...stated as such
		"UNVERIFIED",             // never physically verified
		"not a safety guarantee", // the mandatory disclaimer
	} {
		if !strings.Contains(body, want) {
			t.Errorf("readiness page missing %q", want)
		}
	}
}

func TestKitVerify(t *testing.T) {
	e := newEnv(t)
	e.login(t)
	kit := &model.Kit{Name: "Shelf kit"}
	if err := e.st.CreateKit(ctx, kit); err != nil {
		t.Fatal(err)
	}
	e.post(t, "/kits/"+kit.ID+"/verify", url.Values{})

	got, err := e.st.GetKit(ctx, kit.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastVerified != today() {
		t.Errorf("last_verified = %q, want today", got.LastVerified)
	}
	events, err := e.st.ListMaintenance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != model.MaintInspection || events[0].KitID != kit.ID {
		t.Errorf("verify did not log a maintenance event: %+v", events)
	}
}

func TestDashboardDistinguishesExpiredAndExpiring(t *testing.T) {
	e := newEnv(t)
	e.login(t)

	it := &model.Item{Name: "Chili cans", Category: model.CatFood}
	if err := e.st.CreateItem(ctx, it); err != nil {
		t.Fatal(err)
	}
	if err := e.st.CreateLot(ctx, &model.Lot{ItemID: it.ID, Quantity: 6, Expires: "2020-01-01"}); err != nil {
		t.Fatal(err)
	}
	it2 := &model.Item{Name: "Granola bars", Category: model.CatFood}
	if err := e.st.CreateItem(ctx, it2); err != nil {
		t.Fatal(err)
	}
	if err := e.st.CreateLot(ctx, &model.Lot{ItemID: it2.ID, Quantity: 4, Expires: today()}); err != nil {
		t.Fatal(err)
	}

	_, body := e.get(t, "/")
	expIdx := strings.Index(body, "Expired — still physically present")
	soonIdx := strings.Index(body, "Expiring within 90 days")
	if expIdx == -1 || soonIdx == -1 {
		t.Fatal("dashboard sections missing")
	}
	expSection := body[expIdx:soonIdx]
	soonSection := body[soonIdx:]
	if !strings.Contains(expSection, "Chili cans") || strings.Contains(expSection, "Granola bars") {
		t.Error("expired section wrong")
	}
	if !strings.Contains(soonSection, "Granola bars") || strings.Contains(soonSection, "Chili cans") {
		t.Error("expiring section wrong")
	}
}

func TestQRLabelAndResolve(t *testing.T) {
	e := newEnv(t)
	e.login(t)
	c := &model.Container{Name: "Blue bin"}
	if err := e.st.CreateContainer(ctx, c); err != nil {
		t.Fatal(err)
	}

	resp, err := e.client.Get(e.ts.URL + "/containers/" + c.ID + "/qr.png")
	if err != nil {
		t.Fatal(err)
	}
	png, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Header.Get("Content-Type") != "image/png" || len(png) < 100 {
		t.Errorf("qr.png: type %q, %d bytes", resp.Header.Get("Content-Type"), len(png))
	}

	code, body := e.get(t, "/containers/"+c.ID+"/label")
	if code != http.StatusOK || !strings.Contains(body, c.QRSlug) {
		t.Errorf("label page must print the slug as text")
	}

	// Scanning the code yields the slug; the resolver lands on the container.
	resp2, err := e.client.Get(e.ts.URL + "/qr/" + c.QRSlug)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.Request.URL.Path != "/containers/"+c.ID {
		t.Errorf("qr resolve landed on %s, want the container page", resp2.Request.URL.Path)
	}

	if code, _ := e.get(t, "/qr/PK-XXXX-XXXX"); code != http.StatusNotFound {
		t.Errorf("unknown slug: %d, want 404", code)
	}
}

func TestExportDownloads(t *testing.T) {
	e := newEnv(t)
	e.login(t)
	it := &model.Item{Name: "Radio", Category: model.CatComms, Serial: "SN-42", Quantity: fp(1)}
	if err := e.st.CreateItem(ctx, it); err != nil {
		t.Fatal(err)
	}

	_, redacted := e.get(t, "/exports/inventory.json")
	if !strings.Contains(redacted, `"spec_version": "0.1"`) {
		t.Error("json export malformed")
	}
	if strings.Contains(redacted, "SN-42") {
		t.Error("standard export leaked a serial number")
	}
	// No route restores sensitive fields: threat-model.md forbids a bulk
	// override, and a complete copy is a database backup made on the server.
	gone, err := e.client.Get(e.ts.URL + "/exports/inventory-full.json")
	if err != nil {
		t.Fatal(err)
	}
	gone.Body.Close()
	if gone.StatusCode != http.StatusNotFound {
		t.Errorf("full export route returned %d, want 404", gone.StatusCode)
	}

	resp, err := e.client.Get(e.ts.URL + "/exports/offline.zip")
	if err != nil {
		t.Fatal(err)
	}
	zipBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Header.Get("Content-Type") != "application/zip" || len(zipBytes) < 200 {
		t.Errorf("offline.zip: type %q, %d bytes", resp.Header.Get("Content-Type"), len(zipBytes))
	}
	if string(zipBytes[:2]) != "PK" {
		t.Error("offline.zip is not a zip archive")
	}
}

func fp(v float64) *float64 { return &v }
