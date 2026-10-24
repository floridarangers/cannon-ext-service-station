package main

import (
	"strings"
	"testing"
)

func TestStationIDFromURL(t *testing.T) {
	cases := map[string]string{
		"/service/station1":                     "station1",
		"/service/station2/":                    "station2",
		"/service/station1?sent=1":              "station1",
		"https://example.test/service/station3": "station3",
		"/service/2":                            "2",
		"/service/station-4":                    "station-4",
		"/en/service/station1":                  "station1",
		"/service/submit":                       "",
		"/service":                              "",
		"/contact":                              "",
		"/service/station 1":                    "",
		"":                                      "",
	}
	for raw, want := range cases {
		if got := stationIDFromURL(raw); got != want {
			t.Errorf("stationIDFromURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestApplyStationForcesHiddenValue(t *testing.T) {
	form := stationForm{
		SchemaJSON: defaultSchemaJSON(),
		UISchemaJSON: `{
		  "type": "VerticalLayout",
		  "elements": [
		    {"type": "Control", "scope": "#/properties/station_id"},
		    {"type": "Control", "scope": "#/properties/needs"},
		    {"type": "Control", "scope": "#/properties/note", "options": {"multi": true}}
		  ]
		}`,
	}
	fields := applyStation(serviceFields(form), "station4")
	if len(fields) == 0 {
		t.Fatal("expected fields")
	}
	if fields[0].Name != stationFieldName || !fields[0].Hidden || fields[0].Default != "station4" || fields[0].Type != "hidden" {
		t.Fatalf("station field = %#v", fields[0])
	}
	if !fields[1].Checkboxes || len(fields[1].Options) != 4 {
		t.Fatalf("needs field = %#v", fields[1])
	}
}

func TestNotificationTextIsPlain(t *testing.T) {
	form := stationForm{
		Name:         defaultFormName,
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	raw := `{"station_id":"station1","needs":["Toilet Paper","Cleaning"],"note":"Dispenser is jammed"}`
	fields := submissionFields(form, submissionData(raw))
	text := notificationText("station1", fields)
	for _, want := range []string{
		"Service request for station1",
		"Station: station1",
		"What needs service: Toilet Paper, Cleaning",
		"Note: Dispenser is jammed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("notification missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "{") || strings.Contains(text, "}") || strings.Contains(text, raw) || strings.Contains(text, `"needs"`) {
		t.Fatalf("notification should be plain text, got:\n%s", text)
	}
}

func TestNotificationOmitsUncheckedAndExpandsObjects(t *testing.T) {
	form := stationForm{
		SchemaJSON: `{
		  "type": "object",
		  "properties": {
		    "station_id": {"type": "string", "title": "Station"},
		    "toilet_paper": {"type": "boolean", "title": "Toilet Paper"},
		    "cleaning": {"type": "boolean", "title": "Cleaning"},
		    "details": {"type": "string", "title": "Details"}
		  }
		}`,
		UISchemaJSON: defaultUISchemaJSON(),
	}
	raw := `{"station_id":"station2","toilet_paper":true,"cleaning":false,"details":"{\"area\":\"stall 2\",\"status\":\"empty\"}"}`
	text := notificationText("station2", submissionFields(form, submissionData(raw)))
	if !strings.Contains(text, "Toilet Paper: Yes") {
		t.Fatalf("missing checked box:\n%s", text)
	}
	if strings.Contains(text, "Cleaning") {
		t.Fatalf("unchecked box should be omitted:\n%s", text)
	}
	if strings.Contains(text, "{") || strings.Contains(text, "}") || !strings.Contains(text, "Area: stall 2") || !strings.Contains(text, "Status: empty") {
		t.Fatalf("object should be flattened:\n%s", text)
	}
}

func TestDeliverNotificationUsesShoutrrr(t *testing.T) {
	text := "Service request for station1\nStation: station1\nWhat needs service: Toilet Paper"
	if err := deliverNotification("logger://", text); err != nil {
		t.Fatal(err)
	}
	if err := deliverNotification("  ", text); err == nil {
		t.Fatal("expected missing shoutrrr url to fail")
	}
}
