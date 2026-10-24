package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

type entryView struct {
	ID        uint
	StationID string
	Summary   string
	CreatedAt string
}

func (app *stationApp) admin(req extension.WireRequest) extension.WireResponse {
	if !extension.UserAuthenticated(req) {
		return extension.Error(http.StatusForbidden, "sign in required")
	}
	db, err := app.siteDB()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	adminURL, err := parseWireURL(req.URL)
	if err != nil {
		return extension.Error(http.StatusBadRequest, err.Error())
	}
	basePath, parts := adminRoute(adminURL.Path)
	if basePath == "" {
		basePath = "/admin/extension-apps/" + url.PathEscape(extensionName)
	}
	switch {
	case len(parts) == 0:
		return app.listEntries(db, adminURL, basePath, "", csrfField(req))
	case len(parts) == 1 && parts[0] == "form":
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.saveForm(db, req.Body, adminURL, basePath, csrfField(req))
		}
		return app.editForm(basePath, "", csrfField(req))
	case len(parts) == 2 && parts[0] == "entries":
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "request not found")
		}
		return app.showEntry(db, basePath, id, csrfField(req))
	case len(parts) == 3 && parts[0] == "entries" && parts[2] == "delete":
		id, ok := parseID(parts[1])
		if !ok {
			return extension.Error(http.StatusNotFound, "request not found")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		return app.deleteEntry(db, adminURL, basePath, id, csrfField(req))
	default:
		return extension.Error(http.StatusNotFound, "admin route not found")
	}
}

func (app *stationApp) listEntries(db *gorm.DB, adminURL *url.URL, basePath, notice string, csrf template.HTML) extension.WireResponse {
	form, _, err := app.anyForm()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	page := parsePage(adminURL)
	var total int64
	if err := db.Table(app.entriesTable()).Count(&total).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	var entries []stationEntry
	err = db.Table(app.entriesTable()).
		Order("created_at desc, entry_id desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&entries).Error
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	rows := make([]entryView, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, entryView{
			ID:        entry.ID,
			StationID: entry.StationID,
			Summary:   entrySummary(form, entry.DataJSON),
			CreatedAt: entry.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return app.render("service-station/admin_list.html", map[string]any{
		"Title":       "Service Station",
		"Subtitle":    "Restroom service requests and the public form schema.",
		"Form":        form,
		"HasForm":     form.ID != 0,
		"Rows":        rows,
		"Page":        page,
		"PageSize":    pageSize,
		"Total":       int(total),
		"Notice":      notice,
		"CSRFField":   csrf,
		"BasePath":    basePath,
		"FormURL":     basePath + "/form",
		"SettingsURL": "/admin/configuration/extensions/" + url.PathEscape(extensionName),
	})
}

func (app *stationApp) editForm(basePath, errMsg string, csrf template.HTML) extension.WireResponse {
	row, found, err := app.anyForm()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if !found {
		row = stationForm{
			Name:         defaultFormName,
			ShowName:     true,
			Introduction: defaultIntroduction,
			SchemaJSON:   defaultSchemaJSON(),
			UISchemaJSON: defaultUISchemaJSON(),
			Status:       "active",
		}
	}
	return app.renderForm(basePath, row, errMsg, csrf)
}

func (app *stationApp) saveForm(db *gorm.DB, body string, adminURL *url.URL, basePath string, csrf template.HTML) extension.WireResponse {
	existing, found, err := app.anyForm()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	id := uint(0)
	if found {
		id = existing.ID
	}
	row, errMsg := formFromBody(body, id)
	if errMsg != "" {
		return app.renderForm(basePath, row, errMsg, csrf)
	}
	if found {
		row.CreatedAt = existing.CreatedAt
		if err := db.Table(app.formsTable()).Save(&row).Error; err != nil {
			return app.renderForm(basePath, row, err.Error(), csrf)
		}
	} else if err := db.Table(app.formsTable()).Create(&row).Error; err != nil {
		return app.renderForm(basePath, row, err.Error(), csrf)
	}
	return app.listEntries(db, adminURL, basePath, "Form saved.", csrf)
}

func (app *stationApp) showEntry(db *gorm.DB, basePath string, id uint, csrf template.HTML) extension.WireResponse {
	var entry stationEntry
	err := db.Table(app.entriesTable()).Where("entry_id = ?", id).First(&entry).Error
	if err == gorm.ErrRecordNotFound {
		return extension.Error(http.StatusNotFound, "request not found")
	}
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	form, _, err := app.anyForm()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	fields := submissionFields(form, submissionData(entry.DataJSON))
	return app.render("service-station/admin_entry.html", map[string]any{
		"Title":     "Service request",
		"BasePath":  basePath,
		"Entry":     entry,
		"When":      entry.CreatedAt.Format("2006-01-02 15:04"),
		"Text":      notificationText(entry.StationID, fields),
		"DeleteURL": fmt.Sprintf("%s/entries/%d/delete", basePath, entry.ID),
		"CSRFField": csrf,
	})
}

func (app *stationApp) deleteEntry(db *gorm.DB, adminURL *url.URL, basePath string, id uint, csrf template.HTML) extension.WireResponse {
	if err := db.Table(app.entriesTable()).Where("entry_id = ?", id).Delete(&stationEntry{}).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.listEntries(db, adminURL, basePath, "Request deleted.", csrf)
}

func (app *stationApp) renderForm(basePath string, row stationForm, errMsg string, csrf template.HTML) extension.WireResponse {
	return app.render("service-station/admin_form.html", map[string]any{
		"Title":     "Service form",
		"Subtitle":  "JSON Forms schema used by every /service/{station} page.",
		"BasePath":  basePath,
		"ActionURL": basePath + "/form",
		"CancelURL": basePath,
		"Row":       row,
		"Error":     errMsg,
		"CSRFField": csrf,
	})
}

func formFromBody(body string, id uint) (stationForm, string) {
	values, err := url.ParseQuery(body)
	if err != nil {
		return stationForm{}, err.Error()
	}
	showName := values.Get("show_name")
	row := stationForm{
		ID:           id,
		Name:         strings.TrimSpace(values.Get("name")),
		ShowName:     showName == "1" || strings.EqualFold(showName, "on") || strings.EqualFold(showName, "true") || strings.EqualFold(showName, "yes"),
		Introduction: strings.TrimSpace(values.Get("introduction")),
		SchemaJSON:   strings.TrimSpace(values.Get("schema_json")),
		UISchemaJSON: strings.TrimSpace(values.Get("ui_schema_json")),
		Status:       strings.TrimSpace(values.Get("status")),
	}
	if row.Status != "inactive" {
		row.Status = "active"
	}
	if row.Name == "" {
		return row, "Form name is required."
	}
	if row.SchemaJSON == "" {
		row.SchemaJSON = defaultSchemaJSON()
	}
	if row.UISchemaJSON == "" {
		row.UISchemaJSON = defaultUISchemaJSON()
	}
	if !validJSONObject(row.SchemaJSON) {
		return row, "Schema JSON must be a valid JSON object."
	}
	if !validJSONObject(row.UISchemaJSON) {
		return row, "UI schema JSON must be a valid JSON object."
	}
	return row, ""
}
