package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type serviceField struct {
	Name        string
	Label       string
	Type        string
	SchemaType  string
	Default     string
	Checked     bool
	Textarea    bool
	Select      bool
	Boolean     bool
	Checkboxes  bool
	Hidden      bool
	Options     []serviceFieldOption
	Required    bool
	Readonly    bool
	Placeholder string
	Help        string
	Step        string
}

type serviceFieldOption struct {
	Value    string
	Label    string
	Selected bool
}

type schemaDocument struct {
	Properties map[string]schemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

type schemaProperty struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Format      string          `json:"format"`
	Default     any             `json:"default"`
	Enum        []any           `json:"enum"`
	OneOf       []schemaChoice  `json:"oneOf"`
	AnyOf       []schemaChoice  `json:"anyOf"`
	Items       *schemaProperty `json:"items"`
	Const       any             `json:"const"`
}

type schemaChoice struct {
	Const any    `json:"const"`
	Title string `json:"title"`
}

type uiElement struct {
	Type     string         `json:"type"`
	Scope    string         `json:"scope"`
	Label    any            `json:"label"`
	Options  map[string]any `json:"options"`
	Elements []uiElement    `json:"elements"`
}

func defaultSchemaJSON() string {
	return `{
  "type": "object",
  "properties": {
    "station_id": {
      "type": "string",
      "title": "Station"
    },
    "needs": {
      "type": "array",
      "title": "What needs service",
      "uniqueItems": true,
      "items": {
        "type": "string",
        "enum": ["Toilet Paper", "Hand Sanitizer", "Cleaning", "Other"]
      }
    },
    "note": {
      "type": "string",
      "title": "Note",
      "description": "Optional details for the service request."
    }
  },
  "required": ["station_id"]
}`
}

func defaultUISchemaJSON() string {
	return `{
  "type": "VerticalLayout",
  "elements": [
    {
      "type": "Control",
      "scope": "#/properties/station_id",
      "options": {
        "hidden": true
      }
    },
    {
      "type": "Control",
      "scope": "#/properties/needs"
    },
    {
      "type": "Control",
      "scope": "#/properties/note",
      "options": {
        "multi": true,
        "placeholder": "Optional"
      }
    }
  ]
}`
}

func serviceFields(form stationForm) []serviceField {
	var schema schemaDocument
	if err := json.Unmarshal([]byte(form.SchemaJSON), &schema); err != nil {
		return nil
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	controls := uiControls(form.UISchemaJSON)
	fields := make([]serviceField, 0, len(schema.Properties))
	seen := map[string]bool{}
	for _, control := range controls {
		name := propertyName(control.Scope)
		if name == "" || seen[name] {
			continue
		}
		prop, ok := schema.Properties[name]
		if !ok {
			continue
		}
		options := control.Options
		if _, ok := options["label"]; !ok && control.Label != nil {
			options = copyOptions(options)
			options["label"] = control.Label
		}
		if field, ok := buildServiceField(name, prop, options, required[name]); ok {
			fields = append(fields, field)
			seen[name] = true
		}
	}
	if len(controls) == 0 {
		for _, prop := range orderedSchemaProps(form.SchemaJSON) {
			if seen[prop.Name] {
				continue
			}
			if field, ok := buildServiceField(prop.Name, prop.Prop, nil, required[prop.Name]); ok {
				fields = append(fields, field)
				seen[prop.Name] = true
			}
		}
	}
	return fields
}

func applyStation(fields []serviceField, station string) []serviceField {
	found := false
	for i := range fields {
		if fields[i].Name != stationFieldName {
			continue
		}
		fields[i].Hidden = true
		fields[i].Default = station
		fields[i].Boolean = false
		fields[i].Checkboxes = false
		fields[i].Select = false
		fields[i].Textarea = false
		fields[i].Type = "hidden"
		fields[i].Options = nil
		found = true
	}
	if found {
		return fields
	}
	hidden := serviceField{
		Name:       stationFieldName,
		Label:      "Station",
		Type:       "hidden",
		SchemaType: "string",
		Default:    station,
		Hidden:     true,
		Required:   true,
	}
	return append([]serviceField{hidden}, fields...)
}

func validatedServiceData(form stationForm, values url.Values) (map[string]any, error) {
	fields := serviceFields(form)
	data := make(map[string]any, len(fields))
	for _, field := range fields {
		if field.Name == stationFieldName {
			continue
		}
		value, err := serviceFieldValue(field, values)
		if err != nil {
			return nil, err
		}
		if value != nil {
			data[field.Name] = value
		}
	}
	return data, nil
}

func submissionHasDetail(data map[string]any) bool {
	for name, value := range data {
		if name == stationFieldName {
			continue
		}
		if strings.TrimSpace(formatSubmissionValue(value)) != "" {
			return true
		}
	}
	return false
}

func stationForSubmission(requestURL string, values url.Values) (string, error) {
	station := stationIDFromURL(values.Get("return_url"))
	if station == "" {
		station = stationIDFromURL(requestURL)
	}
	if station == "" {
		station = sanitizeStationID(values.Get(stationFieldName))
	}
	if station == "" {
		return "", fmt.Errorf("station is required")
	}
	return station, nil
}

func stationIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i < len(parts); i++ {
		if !strings.EqualFold(parts[i], "service") || i+1 >= len(parts) {
			continue
		}
		return sanitizeStationID(parts[i+1])
	}
	return ""
}

func sanitizeStationID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || id == "*" || strings.EqualFold(id, "submit") {
		return ""
	}
	if len(id) > 64 {
		return ""
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return id
}

func serviceFieldValue(field serviceField, values url.Values) (any, error) {
	if field.Checkboxes {
		return checkboxValues(field, values)
	}
	raw := strings.TrimSpace(values.Get(field.Name))
	if field.Boolean {
		checked := raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "on") || strings.EqualFold(raw, "yes")
		if field.Required && !checked {
			return nil, fmt.Errorf("%s is required", field.Label)
		}
		return checked, nil
	}
	if field.Required && raw == "" {
		return nil, fmt.Errorf("%s is required", field.Label)
	}
	if raw == "" {
		return nil, nil
	}
	if len(field.Options) > 0 && !fieldAllowsOption(field, raw) {
		return nil, fmt.Errorf("%s has an invalid value", field.Label)
	}
	switch field.SchemaType {
	case "integer":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be a whole number", field.Label)
		}
		return value, nil
	case "number":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%s must be a number", field.Label)
		}
		return value, nil
	default:
		if err := validateStringFormat(field, raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
}

func checkboxValues(field serviceField, values url.Values) (any, error) {
	selected := make([]string, 0, len(values[field.Name]))
	seen := map[string]struct{}{}
	for _, raw := range values[field.Name] {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !fieldAllowsOption(field, raw) {
			return nil, fmt.Errorf("%s has an invalid value", field.Label)
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		selected = append(selected, raw)
	}
	if field.Required && len(selected) == 0 {
		return nil, fmt.Errorf("%s is required", field.Label)
	}
	if len(selected) == 0 {
		return nil, nil
	}
	return selected, nil
}

func fieldAllowsOption(field serviceField, value string) bool {
	for _, option := range field.Options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func validateStringFormat(field serviceField, value string) error {
	switch field.Type {
	case "email":
		if _, err := mail.ParseAddress(value); err != nil {
			return fmt.Errorf("%s must be a valid email address", field.Label)
		}
	case "url":
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%s must be a valid URL", field.Label)
		}
	case "date":
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return fmt.Errorf("%s must be a valid date", field.Label)
		}
	case "datetime-local":
		if _, err := time.Parse("2006-01-02T15:04", value); err != nil {
			return fmt.Errorf("%s must be a valid date and time", field.Label)
		}
	}
	return nil
}

func buildServiceField(name string, prop schemaProperty, options map[string]any, required bool) (serviceField, bool) {
	schemaType := strings.TrimSpace(prop.Type)
	if schemaType == "" {
		schemaType = "string"
	}
	if schemaType == "array" {
		return buildCheckboxField(name, prop, options, required)
	}
	if schemaType != "string" && schemaType != "boolean" && schemaType != "integer" && schemaType != "number" {
		return serviceField{}, false
	}
	inputType := "text"
	step := ""
	switch schemaType {
	case "boolean":
		inputType = "checkbox"
	case "integer":
		inputType = "number"
		step = "1"
	case "number":
		inputType = "number"
		step = "any"
	}
	switch prop.Format {
	case "email":
		inputType = "email"
	case "uri":
		inputType = "url"
	case "date":
		inputType = "date"
	case "date-time":
		inputType = "datetime-local"
	case "tel", "phone":
		inputType = "tel"
	}
	if optionFormat := stringOption(options, "format"); optionFormat == "password" {
		inputType = "password"
	}
	defaultValue := defaultFieldValue(prop.Default)
	field := serviceField{
		Name:        name,
		Label:       fieldLabel(name, prop.Title),
		Type:        inputType,
		SchemaType:  schemaType,
		Default:     defaultValue,
		Checked:     defaultBoolValue(prop.Default),
		Boolean:     schemaType == "boolean",
		Textarea:    boolOption(options, "multi"),
		Hidden:      boolOption(options, "hidden"),
		Required:    required,
		Readonly:    boolOption(options, "readonly") || boolOption(options, "readOnly"),
		Placeholder: stringOption(options, "placeholder"),
		Help:        prop.Description,
		Step:        step,
	}
	if label, ok := stringLabel(options, "label"); ok {
		field.Label = label
	}
	for _, value := range prop.Enum {
		label := fmt.Sprint(value)
		field.Options = append(field.Options, serviceFieldOption{Value: label, Label: label, Selected: label == defaultValue})
	}
	for _, choice := range append(prop.OneOf, prop.AnyOf...) {
		if choice.Const == nil {
			continue
		}
		value := fmt.Sprint(choice.Const)
		label := strings.TrimSpace(choice.Title)
		if label == "" {
			label = value
		}
		field.Options = append(field.Options, serviceFieldOption{Value: value, Label: label, Selected: value == defaultValue})
	}
	if len(field.Options) > 0 {
		field.Select = true
		field.Textarea = false
		field.Boolean = false
		field.Type = "select"
	}
	if field.Hidden {
		field.Type = "hidden"
		field.Select = false
		field.Textarea = false
		field.Boolean = false
	}
	return field, true
}

func buildCheckboxField(name string, prop schemaProperty, options map[string]any, required bool) (serviceField, bool) {
	if prop.Items == nil {
		return serviceField{}, false
	}
	field := serviceField{
		Name:       name,
		Label:      fieldLabel(name, prop.Title),
		Type:       "checkboxes",
		SchemaType: "array",
		Checkboxes: true,
		Required:   required,
		Help:       prop.Description,
		Hidden:     boolOption(options, "hidden"),
	}
	if label, ok := stringLabel(options, "label"); ok {
		field.Label = label
	}
	for _, value := range prop.Items.Enum {
		label := fmt.Sprint(value)
		if strings.TrimSpace(label) == "" {
			continue
		}
		field.Options = append(field.Options, serviceFieldOption{Value: label, Label: label})
	}
	for _, choice := range append(prop.Items.OneOf, prop.Items.AnyOf...) {
		if choice.Const == nil {
			continue
		}
		value := fmt.Sprint(choice.Const)
		label := strings.TrimSpace(choice.Title)
		if label == "" {
			label = value
		}
		field.Options = append(field.Options, serviceFieldOption{Value: value, Label: label})
	}
	if len(field.Options) == 0 {
		return serviceField{}, false
	}
	return field, true
}

func defaultFieldValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func defaultBoolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || strings.EqualFold(v, "on") || strings.EqualFold(v, "yes") || v == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}

func copyOptions(options map[string]any) map[string]any {
	out := make(map[string]any, len(options)+1)
	for key, value := range options {
		out[key] = value
	}
	return out
}

func uiControls(raw string) []uiElement {
	var root uiElement
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	var out []uiElement
	walkUI(root, &out)
	return out
}

func walkUI(el uiElement, out *[]uiElement) {
	if propertyName(el.Scope) != "" {
		*out = append(*out, el)
	}
	for _, child := range el.Elements {
		walkUI(child, out)
	}
}

func propertyName(scope string) string {
	name := strings.TrimPrefix(scope, "#/properties/")
	if name == scope || name == "" || strings.Contains(name, "/") {
		return ""
	}
	return name
}

func boolOption(options map[string]any, key string) bool {
	if options == nil {
		return false
	}
	v, _ := options[key].(bool)
	return v
}

func stringOption(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	v, _ := options[key].(string)
	return strings.TrimSpace(v)
}

func stringLabel(options map[string]any, key string) (string, bool) {
	if options == nil {
		return "", false
	}
	raw, ok := options[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), strings.TrimSpace(v) != ""
	case bool:
		if !v {
			return "", false
		}
	}
	return "", false
}

func fieldLabel(name, title string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	if name == stationFieldName {
		return "Station"
	}
	parts := strings.Fields(strings.ReplaceAll(name, "_", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func validJSONObject(raw string) bool {
	var value map[string]any
	return json.Unmarshal([]byte(raw), &value) == nil
}

type namedSchemaProp struct {
	Name string
	Prop schemaProperty
}

func orderedSchemaProps(raw string) []namedSchemaProp {
	var envelope struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || len(envelope.Properties) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(envelope.Properties))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	var out []namedSchemaProp
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		name, ok := tok.(string)
		if !ok {
			break
		}
		var prop schemaProperty
		if err := dec.Decode(&prop); err != nil {
			break
		}
		out = append(out, namedSchemaProp{Name: name, Prop: prop})
	}
	return out
}
