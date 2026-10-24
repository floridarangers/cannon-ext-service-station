package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/containrrr/shoutrrr"
)

type submissionField struct {
	Name  string
	Label string
	Value string
}

func (app *stationApp) notifySubmission(settings stationSettings, form stationForm, entry stationEntry) {
	text := notificationText(entry.StationID, submissionFields(form, submissionData(entry.DataJSON)))
	if err := deliverNotification(settings.ShoutrrrURL, text); err != nil {
		log.Printf("service-station: notify %s: %v", entry.StationID, err)
	}
}

func deliverNotification(rawURL, text string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("shoutrrr_url is not configured")
	}
	return shoutrrr.Send(rawURL, text)
}

func notificationText(station string, fields []submissionField) string {
	var b strings.Builder
	station = strings.TrimSpace(station)
	if station == "" {
		b.WriteString("Service request")
	} else {
		fmt.Fprintf(&b, "Service request for %s", station)
	}
	for _, field := range fields {
		fmt.Fprintf(&b, "\n%s: %s", field.Label, field.Value)
	}
	return b.String()
}

func submissionData(raw string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data == nil {
		return map[string]any{}
	}
	return data
}

func submissionFields(form stationForm, data map[string]any) []submissionField {
	labels := map[string]string{}
	for _, field := range serviceFields(form) {
		labels[field.Name] = field.Label
	}
	var ordered []string
	seen := map[string]bool{}
	for _, field := range serviceFields(form) {
		if seen[field.Name] {
			continue
		}
		ordered = append(ordered, field.Name)
		seen[field.Name] = true
	}
	for _, prop := range orderedSchemaProps(form.SchemaJSON) {
		if seen[prop.Name] {
			continue
		}
		if _, ok := labels[prop.Name]; !ok {
			labels[prop.Name] = fieldLabel(prop.Name, prop.Prop.Title)
		}
		ordered = append(ordered, prop.Name)
		seen[prop.Name] = true
	}
	for name := range data {
		if seen[name] {
			continue
		}
		labels[name] = fieldLabel(name, "")
		ordered = append(ordered, name)
		seen[name] = true
	}
	out := make([]submissionField, 0, len(ordered))
	for _, name := range ordered {
		value, ok := data[name]
		if !ok || value == nil {
			continue
		}
		text := formatSubmissionValue(value)
		if strings.TrimSpace(text) == "" {
			continue
		}
		label := labels[name]
		if strings.TrimSpace(label) == "" {
			label = name
		}
		out = append(out, submissionField{Name: name, Label: label, Value: text})
	}
	return out
}

func entrySummary(form stationForm, raw string) string {
	var parts []string
	for _, field := range submissionFields(form, submissionData(raw)) {
		if field.Name == stationFieldName {
			continue
		}
		parts = append(parts, field.Label+": "+oneLine(field.Value))
	}
	return strings.Join(parts, " · ")
}

func oneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func formatSubmissionValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return expandJSONValue(v)
	case bool:
		if v {
			return "Yes"
		}
		return ""
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	case []string:
		return joinPlain(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text := formatSubmissionValue(item)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		return flattenMap(v)
	default:
		return expandJSONValue(fmt.Sprint(v))
	}
}

func joinPlain(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

func expandJSONValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return value
	}
	var object map[string]any
	if json.Unmarshal([]byte(trimmed), &object) == nil && len(object) > 0 {
		return flattenMap(object)
	}
	var list []any
	if json.Unmarshal([]byte(trimmed), &list) == nil && len(list) > 0 {
		return formatSubmissionValue(list)
	}
	return value
}

func flattenMap(object map[string]any) string {
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		text := formatSubmissionValue(object[name])
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines = append(lines, fieldLabel(name, "")+": "+text)
	}
	return strings.Join(lines, "\n")
}
