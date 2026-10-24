package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rob121/cannon/extension"
)

func TestCapabilities(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	rec := httptest.NewRecorder()
	newServer().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Capabilities map[string]string `json:"capabilities"`
		Defaults     struct {
			Admin struct {
				MenuName string `json:"menu_name"`
			} `json:"admin"`
		} `json:"defaults"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"page":          "/page",
		"admin":         "/admin",
		"help":          "/help",
		"configuration": "/configuration",
		"endpoint":      "/endpoint",
		"data":          "/data",
		"templates":     "/templates",
	}
	for key, path := range want {
		if got.Capabilities[key] != path {
			t.Fatalf("capability %s = %q, want %q", key, got.Capabilities[key], path)
		}
	}
	if _, ok := got.Capabilities["block"]; ok {
		t.Fatal("service station does not expose a block")
	}
	if got.Defaults.Admin.MenuName != defaultAdminMenu {
		t.Fatalf("menu = %q", got.Defaults.Admin.MenuName)
	}
}

func TestMeta(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/meta", nil)
	rec := httptest.NewRecorder()
	newServer().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Name          string `json:"name"`
		Title         string `json:"title"`
		Version       string `json:"version"`
		UpdateURLBase string `json:"update_url_base"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != extensionName || got.Title != extensionTitle || got.Version != extensionVersion || got.UpdateURLBase != updateURLBase {
		t.Fatalf("meta = %#v", got)
	}
}

func TestTemplatesList(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/templates", nil)
	rec := httptest.NewRecorder()
	newServer().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got extension.TemplateListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"service-station/form.html":        "extension/service-station/form.html",
		"service-station/admin_list.html":  "extension/service-station/admin_list.html",
		"service-station/admin_form.html":  "extension/service-station/admin_form.html",
		"service-station/admin_entry.html": "extension/service-station/admin_entry.html",
	}
	if len(got.Templates) != len(want) {
		t.Fatalf("templates = %#v", got.Templates)
	}
	for _, tmpl := range got.Templates {
		if tmpl.OverridePath != want[tmpl.Path] {
			t.Fatalf("template %q override = %q", tmpl.Path, tmpl.OverridePath)
		}
	}
}

func TestInstallCreatesDefaultForm(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	resp := postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("install: %+v", resp)
	}
	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"service_station_settings", "service_station_forms", "service_station_entries"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing table %s", table)
		}
	}
	var form stationForm
	if err := db.Table("service_station_forms").First(&form).Error; err != nil {
		t.Fatal(err)
	}
	if form.Name != defaultFormName || form.Status != "active" || !form.ShowName {
		t.Fatalf("default form = %#v", form)
	}
	if !strings.Contains(form.SchemaJSON, `"station_id"`) || !strings.Contains(form.SchemaJSON, "Toilet Paper") {
		t.Fatalf("schema = %s", form.SchemaJSON)
	}
}

func TestConfigurationSupportsShoutrrrURL(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	req := httptest.NewRequest(http.MethodGet, "/configuration", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var doc extension.ConfigurationDocument
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Sections) != 1 || !bytes.Contains(doc.Sections[0].Schema, []byte(`"shoutrrr_url"`)) {
		t.Fatalf("sections = %#v", doc.Sections)
	}
	body := bytes.NewReader([]byte(`{"section":"general","data":{"shoutrrr_url":"logger://"}}`))
	req = httptest.NewRequest(http.MethodPost, "/configuration", body)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save status %d: %s", rec.Code, rec.Body.String())
	}
	settings, err := loadStationSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ShoutrrrURL != "logger://" {
		t.Fatalf("shoutrrr url = %q", settings.ShoutrrrURL)
	}
}

func TestPageRendersStationFromURL(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	resp := postWire(t, server, "/page/service-station", extension.WireRequest{
		Method:   http.MethodGet,
		URL:      "/service/station1",
		CSRF:     "page-token",
		SiteID:   "example",
		PageItem: "service-station",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, resp.Body)
	}
	action := extension.PublicDataURL(extensionName, "example", "service/submit")
	for _, want := range []string{
		`data-station-id="station1"`,
		`type="hidden" name="station_id" value="station1"`,
		`name="needs" type="checkbox" value="Toilet Paper"`,
		`value="Hand Sanitizer"`,
		`value="Cleaning"`,
		`value="Other"`,
		`name="note"`,
		`placeholder="Optional"`,
		`<textarea`,
		`action="` + action + `"`,
		`name="return_url" value="/service/station1"`,
		`name="_csrf" value="page-token"`,
		`<captcha context="form" provider="any"></captcha>`,
		defaultFormName,
		"Station <strong>station1</strong>",
	} {
		if !strings.Contains(resp.Body, want) {
			t.Fatalf("page missing %q\n%s", want, resp.Body)
		}
	}
	if strings.Contains(resp.Body, `type="text" name="station_id"`) || strings.Contains(resp.Body, `name="station_id" type="checkbox"`) {
		t.Fatalf("station should not be a visible field:\n%s", resp.Body)
	}

	other := postWire(t, server, "/page/service-station", extension.WireRequest{
		Method:   http.MethodGet,
		URL:      "/service/station2/",
		SiteID:   "example",
		PageItem: "service-station",
	})
	if !strings.Contains(other.Body, `value="station2"`) || strings.Contains(other.Body, `value="station1"`) {
		t.Fatalf("station2 page = %s", other.Body)
	}
}

func TestPageWithoutStationDoesNotRenderForm(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})
	resp := postWire(t, server, "/page/service-station", extension.WireRequest{
		Method:   http.MethodGet,
		URL:      "/service",
		SiteID:   "example",
		PageItem: "service-station",
	})
	if !strings.Contains(resp.Body, stationMissingMessage) || strings.Contains(resp.Body, "<form") {
		t.Fatalf("body = %s", resp.Body)
	}
}

func TestSubmitUsesStationFromURL(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})
	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	var form stationForm
	if err := db.Table("service_station_forms").First(&form).Error; err != nil {
		t.Fatal(err)
	}

	values := url.Values{}
	values.Set("form_id", fmt.Sprint(form.ID))
	values.Set("station_id", "station9")
	values.Add("needs", "Toilet Paper")
	values.Add("needs", "Other")
	values.Set("note", "Please restock")
	values.Set("return_url", "/service/station2")
	resp := postWire(t, server, "/data/service/submit", extension.WireRequest{
		Method: http.MethodPost,
		URL:    "/ext/hash/service/submit",
		Body:   values.Encode(),
		SiteID: "example",
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("submit = %+v", resp)
	}
	if loc := resp.Header["Location"]; len(loc) != 1 || loc[0] != "/service/station2?sent=1" {
		t.Fatalf("location = %#v", resp.Header)
	}
	var entry stationEntry
	if err := db.Table("service_station_entries").First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.StationID != "station2" {
		t.Fatalf("stored station = %q", entry.StationID)
	}
	if strings.Contains(entry.DataJSON, "station9") || !strings.Contains(entry.DataJSON, `"station_id":"station2"`) {
		t.Fatalf("stored json = %s", entry.DataJSON)
	}
	if !strings.Contains(entry.DataJSON, "Toilet Paper") || !strings.Contains(entry.DataJSON, "Please restock") {
		t.Fatalf("stored json = %s", entry.DataJSON)
	}
	text := notificationText(entry.StationID, submissionFields(form, submissionData(entry.DataJSON)))
	if strings.Contains(text, entry.DataJSON) || strings.Contains(text, "{") {
		t.Fatalf("notification leaked json:\n%s", text)
	}

	list := postWire(t, server, "/admin/", adminWire(http.MethodGet, adminPath("")))
	if !strings.Contains(list.Body, "station2") || !strings.Contains(list.Body, "Toilet Paper") || strings.Contains(list.Body, entry.DataJSON) {
		t.Fatalf("admin list = %s", list.Body)
	}
	detail := postWire(t, server, "/admin/entries/1", adminWire(http.MethodGet, adminPath("/entries/"+fmt.Sprint(entry.ID))))
	if !strings.Contains(detail.Body, "Service request for station2") || strings.Contains(detail.Body, entry.DataJSON) {
		t.Fatalf("admin detail = %s", detail.Body)
	}
}

func TestSubmitRejectsEmptyRequest(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})
	values := url.Values{}
	values.Set("station_id", "station1")
	values.Set("return_url", "/service/station1")
	resp := postWire(t, server, "/data/service/submit", extension.WireRequest{
		Method: http.MethodPost,
		URL:    "/service/station1",
		Body:   values.Encode(),
		SiteID: "example",
	})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(resp.Body, stationDetailMessage) {
		t.Fatalf("empty submit = %+v", resp)
	}

	values.Set("needs", "Gold faucet")
	resp = postWire(t, server, "/data/service/submit", extension.WireRequest{
		Method: http.MethodPost,
		URL:    "/service/station1",
		Body:   values.Encode(),
		SiteID: "example",
	})
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(resp.Body, "invalid value") {
		t.Fatalf("bad checkbox = %+v", resp)
	}
}

func TestSubmitAllowsNoteOnly(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})
	values := url.Values{}
	values.Set("return_url", "/service/station1")
	values.Set("note", "Lights are out")
	resp := postWire(t, server, "/endpoint/submit", extension.WireRequest{
		Method: http.MethodPost,
		URL:    "/service/submit",
		Body:   values.Encode(),
		SiteID: "example",
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("note-only submit = %+v", resp)
	}
	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	var entry stationEntry
	if err := db.Table("service_station_entries").First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.StationID != "station1" || !strings.Contains(entry.DataJSON, "Lights are out") {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestAdminListAndSchemaValidation(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	denied := postWire(t, server, "/admin/", extension.WireRequest{Method: http.MethodGet, URL: adminPath(""), SiteID: "example"})
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %+v", denied)
	}

	list := postWire(t, server, "/admin/", adminWire(http.MethodGet, adminPath("")))
	if list.StatusCode != http.StatusOK || !strings.Contains(list.Body, defaultFormName) || !strings.Contains(list.Body, "No service requests yet") {
		t.Fatalf("list = %s", list.Body)
	}

	form := url.Values{}
	form.Set("name", "Broken")
	form.Set("status", "active")
	form.Set("schema_json", "{")
	form.Set("ui_schema_json", defaultUISchemaJSON())
	invalid := postWire(t, server, "/admin/form", adminWireWithBody(http.MethodPost, adminPath("/form"), form.Encode()))
	if !strings.Contains(invalid.Body, "Schema JSON must be a valid JSON object.") {
		t.Fatalf("validation = %s", invalid.Body)
	}
}

func setupExtensionDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sites.json")
	dbPath := filepath.Join(dir, "example.sqlite")
	raw := fmt.Sprintf(`{
  "install_enabled": false,
  "data_root": %q,
  "sites": [{
    "id": "example",
    "name": "Example",
    "host": "http://localhost:8001",
    "database": {"type": "sqlite", "dsn": %q},
    "template_dir": %q,
    "assets_dir": %q,
    "tmp_dir": %q,
    "language_dir": %q
  }]
}`, dir, dbPath, filepath.Join(dir, "templates"), filepath.Join(dir, "assets"), filepath.Join(dir, "tmp"), filepath.Join(dir, "language"))
	if err := os.WriteFile(cfgPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CANNON_CONFIG", cfgPath)
	oldArgs := os.Args
	os.Args = []string{"cannon-ext-service-station", "--site=example", "--socket=/tmp/service-station.sock"}
	t.Cleanup(func() { os.Args = oldArgs })
}

func postWire(t *testing.T, handler http.Handler, path string, wire extension.WireRequest) extension.WireResponse {
	t.Helper()
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var resp extension.WireResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func adminWire(method, rawURL string) extension.WireRequest {
	return adminWireWithBody(method, rawURL, "")
}

func adminWireWithBody(method, rawURL, body string) extension.WireRequest {
	return extension.WireRequest{
		Method: method,
		URL:    rawURL,
		Body:   body,
		CSRF:   "admin-token",
		SiteID: "example",
		User: map[string]any{
			"authenticated": true,
		},
	}
}

func adminPath(suffix string) string {
	return "/admin/extension-apps/" + extensionName + suffix
}
