package main

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

//go:embed help/*.md
var helpFiles embed.FS

//go:embed all:templates
var templateFiles embed.FS

const (
	extensionName        = "cannon-ext-service-station"
	extensionTitle       = "Service Station"
	extensionDescription = "Restroom service request forms. The station id comes from /service/{station} URLs, the public form is rendered from a JSON Forms schema, and notifications are plain-text Shoutrrr messages."
	extensionVersion     = "0.1.0"
	updateURLBase        = "https://github.com/floridarangers/cannon-ext-service-station/releases/download"
	defaultAdminMenu     = "Service Station"
	pageSize             = 20

	stationFieldName      = "station_id"
	defaultFormName       = "Restroom service request"
	defaultIntroduction   = "Choose what this restroom needs. A note is optional."
	stationMissingMessage = "Open a station address such as /service/station1."
	stationClosedMessage  = "Service requests are currently unavailable."
	stationDetailMessage  = "Select what needs service or add a note."
)

var serviceConfigSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "shoutrrr_url": {
      "type": "string",
      "title": "Shoutrrr URL",
      "description": "Shoutrrr notification URL. Service requests are sent as plain text, not JSON."
    }
  }
}`)

var serviceConfigUI = json.RawMessage(`{
  "type": "VerticalLayout",
  "elements": [
    {
      "type": "Control",
      "scope": "#/properties/shoutrrr_url"
    }
  ]
}`)

type stationApp struct {
	info          extension.Info
	templates     *extension.Templates
	settingsTable string

	dbMu sync.Mutex
	db   *gorm.DB

	migrateMu sync.Mutex
	migrated  bool

	settingsMu     sync.RWMutex
	settingsCache  stationSettings
	settingsCached bool
}

type stationForm struct {
	ID           uint      `gorm:"primaryKey;column:form_id"`
	Name         string    `gorm:"column:name;size:160;not null"`
	ShowName     bool      `gorm:"column:show_name;not null"`
	Introduction string    `gorm:"column:introduction;type:text"`
	SchemaJSON   string    `gorm:"column:schema_json;type:text;not null"`
	UISchemaJSON string    `gorm:"column:ui_schema_json;type:text;not null"`
	Status       string    `gorm:"column:status;size:16;not null;default:active"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

type stationEntry struct {
	ID        uint      `gorm:"primaryKey;column:entry_id"`
	FormID    uint      `gorm:"column:form_id;index;not null"`
	StationID string    `gorm:"column:station_id;size:64;index;not null"`
	DataJSON  string    `gorm:"column:data_json;type:text;not null"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

type stationSettings struct {
	ShoutrrrURL string `json:"shoutrrr_url"`
}

func main() {
	if err := newServer().Run(); err != nil {
		log.Fatal(err)
	}
}

func newServer() *extension.Server {
	info := extension.Info{
		Name:          extensionName,
		Title:         extensionTitle,
		Description:   extensionDescription,
		Version:       extensionVersion,
		UpdateURLBase: updateURLBase,
		AdminMenuName: defaultAdminMenu,
	}
	app := &stationApp{
		info: info,
		templates: extension.NewTemplates(templateFiles, "templates").WithFuncs(template.FuncMap{
			"add":       func(a, b int) int { return a + b },
			"sub":       func(a, b int) int { return a - b },
			"mul":       func(a, b int) int { return a * b },
			"pageCount": pageCount,
		}),
		settingsTable: extension.TableName(info.TablePrefix(), "settings"),
	}

	s := extension.New(info)
	s.EmbedHelp(helpFiles, "help")
	s.EmbedTemplates(templateFiles, "templates")
	s.OnInstall(app.install)
	s.OnConfiguration(extension.MapConfiguration([]extension.ConfigurationDefinition{
		{
			ID:       "general",
			Title:    "General",
			Schema:   serviceConfigSchema,
			UISchema: serviceConfigUI,
		},
	}, configStore{table: app.settingsTable, app: app}))
	s.RegisterPage(extension.PageDefinition{
		ID:    "service-station",
		Title: "Service Station",
	}, app.page)
	s.RegisterEndpoint(extension.EndpointDefinition{
		ID:    "submit",
		Title: "Submit Service Request",
	}, app.submit)
	s.HandleData("service/submit", func(req extension.WireRequest) extension.WireResponse {
		return app.submit("service/submit", req)
	})
	s.HandleAdmin("/admin/", app.admin)
	return s
}

func (app *stationApp) install(req extension.WireRequest) error {
	_, err := app.siteDB()
	return err
}

func (app *stationApp) siteDB() (*gorm.DB, error) {
	app.dbMu.Lock()
	if app.db != nil {
		db := app.db
		app.dbMu.Unlock()
		return db, nil
	}
	db, _, err := extension.OpenDB()
	if err != nil {
		app.dbMu.Unlock()
		return nil, err
	}
	app.db = db
	app.dbMu.Unlock()
	if err := app.ensureSchema(db); err != nil {
		return nil, err
	}
	return db, nil
}

func (app *stationApp) ensureSchema(db *gorm.DB) error {
	app.migrateMu.Lock()
	defer app.migrateMu.Unlock()
	if app.migrated {
		return nil
	}
	if err := extension.MigrateConfigurationStore(db, app.settingsTable); err != nil {
		return err
	}
	if err := db.Table(app.formsTable()).AutoMigrate(&stationForm{}); err != nil {
		return err
	}
	if err := db.Table(app.entriesTable()).AutoMigrate(&stationEntry{}); err != nil {
		return err
	}
	if err := app.ensureDefaultForm(db); err != nil {
		return err
	}
	app.migrated = true
	return nil
}

func (app *stationApp) ensureDefaultForm(db *gorm.DB) error {
	var count int64
	if err := db.Table(app.formsTable()).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	row := stationForm{
		Name:         defaultFormName,
		ShowName:     true,
		Introduction: defaultIntroduction,
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
		Status:       "active",
	}
	return db.Table(app.formsTable()).Create(&row).Error
}

func (app *stationApp) page(item string, req extension.WireRequest) extension.WireResponse {
	station := stationIDFromURL(req.URL)
	if station == "" {
		return app.render("service-station/form.html", map[string]any{
			"Message": stationMissingMessage,
		})
	}
	form, found, err := app.activeForm()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if !found {
		return app.render("service-station/form.html", map[string]any{
			"Message":   stationClosedMessage,
			"StationID": station,
		})
	}
	return app.render("service-station/form.html", map[string]any{
		"Form":      form,
		"Fields":    applyStation(serviceFields(form), station),
		"ActionURL": extension.PublicDataURL(extensionName, req.SiteID, "service/submit"),
		"ReturnURL": serviceReturnURL(req.URL),
		"Sent":      serviceSent(req.URL),
		"CSRFField": csrfField(req),
		"StationID": station,
		"PageItem":  item,
	})
}

func (app *stationApp) submit(item string, req extension.WireRequest) extension.WireResponse {
	if !strings.EqualFold(req.Method, http.MethodPost) {
		return extension.Error(http.StatusMethodNotAllowed, "POST required")
	}
	values, err := url.ParseQuery(req.Body)
	if err != nil {
		return extension.Error(http.StatusBadRequest, "invalid form submission")
	}

	form, found, err := app.submittedForm(values)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if !found {
		return extension.Error(http.StatusNotFound, "service form not found")
	}

	data, err := validatedServiceData(form, values)
	if err != nil {
		return extension.Error(http.StatusBadRequest, err.Error())
	}
	station, err := stationForSubmission(req.URL, values)
	if err != nil {
		return extension.Error(http.StatusBadRequest, err.Error())
	}
	if !submissionHasDetail(data) {
		return extension.Error(http.StatusBadRequest, stationDetailMessage)
	}
	data[stationFieldName] = station

	raw, err := json.Marshal(data)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	db, err := app.siteDB()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	entry := stationEntry{
		FormID:    form.ID,
		StationID: station,
		DataJSON:  string(raw),
	}
	if err := db.Table(app.entriesTable()).Create(&entry).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}

	settings, err := app.loadSettings()
	if err == nil && strings.TrimSpace(settings.ShoutrrrURL) != "" {
		go app.notifySubmission(settings, form, entry)
	}

	redirectURL := strings.TrimSpace(values.Get("return_url"))
	if redirectURL == "" {
		redirectURL = req.URL
	}
	return extension.Redirect(http.StatusSeeOther, serviceSubmitRedirect(redirectURL))
}

func (app *stationApp) submittedForm(values url.Values) (stationForm, bool, error) {
	rawID := strings.TrimSpace(values.Get("form_id"))
	if rawID == "" {
		return app.activeForm()
	}
	id, err := strconv.Atoi(rawID)
	if err != nil || id <= 0 {
		return stationForm{}, false, nil
	}
	return app.formByID(uint(id), true)
}

func (app *stationApp) activeForm() (stationForm, bool, error) {
	db, err := app.siteDB()
	if err != nil {
		return stationForm{}, false, err
	}
	var form stationForm
	err = db.Table(app.formsTable()).Where("status = ?", "active").Order("form_id asc").First(&form).Error
	if err == gorm.ErrRecordNotFound {
		return stationForm{}, false, nil
	}
	if err != nil {
		return stationForm{}, false, err
	}
	return form, true, nil
}

func (app *stationApp) anyForm() (stationForm, bool, error) {
	db, err := app.siteDB()
	if err != nil {
		return stationForm{}, false, err
	}
	var form stationForm
	err = db.Table(app.formsTable()).Order("form_id asc").First(&form).Error
	if err == gorm.ErrRecordNotFound {
		return stationForm{}, false, nil
	}
	if err != nil {
		return stationForm{}, false, err
	}
	return form, true, nil
}

func (app *stationApp) formByID(id uint, activeOnly bool) (stationForm, bool, error) {
	db, err := app.siteDB()
	if err != nil {
		return stationForm{}, false, err
	}
	query := db.Table(app.formsTable()).Where("form_id = ?", id)
	if activeOnly {
		query = query.Where("status = ?", "active")
	}
	var form stationForm
	err = query.First(&form).Error
	if err == gorm.ErrRecordNotFound {
		return stationForm{}, false, nil
	}
	if err != nil {
		return stationForm{}, false, err
	}
	return form, true, nil
}

func csrfField(req extension.WireRequest) template.HTML {
	return template.HTML(extension.CSRFHiddenField(req))
}

func (app *stationApp) render(name string, data map[string]any) extension.WireResponse {
	body, err := app.templates.Execute(name, data)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return extension.HTML(http.StatusOK, body)
}

func (app *stationApp) formsTable() string {
	return extension.TableName(app.info.TablePrefix(), "forms")
}

func (app *stationApp) entriesTable() string {
	return extension.TableName(app.info.TablePrefix(), "entries")
}

type configStore struct {
	table string
	app   *stationApp
}

func (s configStore) Load(section string) (map[string]any, error) {
	var db *gorm.DB
	var err error
	if s.app != nil {
		db, err = s.app.siteDB()
	} else {
		db, _, err = extension.OpenDB()
	}
	if err != nil {
		return nil, err
	}
	return extension.DBConfigurationStore(db, s.table).Load(section)
}

func (s configStore) Save(section string, data map[string]any) error {
	var db *gorm.DB
	var err error
	if s.app != nil {
		db, err = s.app.siteDB()
	} else {
		db, _, err = extension.OpenDB()
	}
	if err != nil {
		return err
	}
	if err := extension.DBConfigurationStore(db, s.table).Save(section, data); err != nil {
		return err
	}
	if s.app != nil {
		s.app.invalidateSettings()
	}
	return nil
}

func (app *stationApp) loadSettings() (stationSettings, error) {
	app.settingsMu.RLock()
	if app.settingsCached {
		cfg := app.settingsCache
		app.settingsMu.RUnlock()
		return cfg, nil
	}
	app.settingsMu.RUnlock()

	data, err := (configStore{table: app.settingsTable, app: app}).Load("general")
	if err != nil {
		return stationSettings{}, err
	}
	cfg := stationSettings{ShoutrrrURL: strings.TrimSpace(stringValue(data, "shoutrrr_url"))}
	app.settingsMu.Lock()
	app.settingsCache = cfg
	app.settingsCached = true
	app.settingsMu.Unlock()
	return cfg, nil
}

func (app *stationApp) invalidateSettings() {
	app.settingsMu.Lock()
	app.settingsCached = false
	app.settingsCache = stationSettings{}
	app.settingsMu.Unlock()
}

func loadStationSettings() (stationSettings, error) {
	store := configStore{table: extension.TableName(extension.Info{Name: extensionName}.TablePrefix(), "settings")}
	data, err := store.Load("general")
	if err != nil {
		return stationSettings{}, err
	}
	return stationSettings{ShoutrrrURL: strings.TrimSpace(stringValue(data, "shoutrrr_url"))}, nil
}

func stringValue(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, _ := data[key].(string)
	return value
}

func serviceSubmitRedirect(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(u.Path) == "" {
		return "/?sent=1"
	}
	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/submit") {
		path = strings.TrimSuffix(path, "/submit")
	}
	if path == "" {
		path = "/"
	}
	q := u.Query()
	q.Set("sent", "1")
	u.RawQuery = q.Encode()
	u.Path = path
	return u.String()
}

func serviceSent(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.TrimSpace(u.Query().Get("sent")) == "1"
}

func serviceReturnURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(u.Path) == "" {
		return "/"
	}
	q := u.Query()
	q.Del("sent")
	u.RawQuery = q.Encode()
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

func parsePage(u *url.URL) int {
	page, err := strconv.Atoi(u.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func pageCount(total int, size int) int {
	if total <= 0 || size <= 0 {
		return 1
	}
	return (total + size - 1) / size
}

func parseWireURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return url.Parse("/admin/extension-apps/" + url.PathEscape(extensionName))
	}
	return url.Parse(raw)
}

func adminRoute(path string) (string, []string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "admin" && parts[i+1] == "extension-apps" {
			base := "/" + strings.Join(parts[:i+3], "/")
			return base, parts[i+3:]
		}
	}
	return "", nil
}

func parseID(value string) (uint, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}
