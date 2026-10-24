# Cannon Service Station Extension

Restroom service requests for Cannon. Pages such as `/service/station1` and `/service/station2` render one JSON Forms schema. The station id is taken from the URL and submitted as a hidden field. Notifications go out through [Shoutrrr](https://github.com/containrrr/shoutrrr) as plain text.

## Features

- One public form for every station URL.
- JSON Forms schema and UI schema, editable in the extension admin.
- Default checkboxes for Toilet Paper, Hand Sanitizer, Cleaning, and Other, plus an optional note.
- Hidden `station_id` filled from the path segment after `/service/`.
- Stored requests.
- Optional Shoutrrr notification URL. The message lists the answers in plain text and does not send the JSON payload.
- Overridable templates.

## Cannon capabilities

- `/meta`
- `/page` — `service-station`
- `/data/service/submit`
- `/endpoint` — `submit`
- `/admin`
- `/configuration` — `shoutrrr_url`
- `/templates`
- `/help`
- `/install`

## Usage

1. Install and activate `cannon-ext-service-station`.
2. Set a Shoutrrr URL under the extension configuration. Leave it blank to save requests without notifying.
3. Create a route with path `/service/*`, type Extension, extension Service Station, and page Service Station.
4. Open `/service/station1`. The form shows the station and posts `station_id=station1` as a hidden field.

Service URLs are documented at [containrrr.dev/shoutrrr](https://containrrr.dev/shoutrrr/).

## Default notification

```text
Service request for station1
Station: station1
What needs service: Toilet Paper, Cleaning
Note: Dispenser is jammed
```

## Releases

- Name: `cannon-ext-service-station`
- Version: `0.1.0`
- Update URL base: `https://github.com/floridarangers/cannon-ext-service-station/releases/download`

GitHub releases should include a `cannon-extension.json` manifest and platform binaries.
