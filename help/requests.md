# Service Requests

## Public route

Create one Cannon route under **System -> Routes**:

- Path: `/service/*`
- Type: Extension
- Extension: Service Station
- Page: Service Station (`service-station`)

These addresses then render the same form, with a different hidden station id:

- `/service/station1`
- `/service/station2`
- `/service/station3`

The station id is the path segment after `/service/`. Fixed routes such as `/service/station1` work the same way. A page without that segment does not render the form.

The public form posts to:

```text
/ext/{route_hash}/service/submit
```

The handler stores the request, sends the Shoutrrr notification when a URL is configured, and redirects back to the station page with `sent=1`.

The station saved on the request comes from the page URL. A changed hidden field does not override a station id that is present in the return URL.

## Default form

The default JSON Forms schema includes:

- `station_id` — hidden, set from the URL
- `needs` — checkboxes for Toilet Paper, Hand Sanitizer, Cleaning, and Other
- `note` — optional text

Edit the schema from the extension admin. Keep a string property named `station_id` so the page can pass the station through the form. An array property with string `enum` items renders as checkboxes. A string property with the UI option `"multi": true` renders as a textarea.

A request must include at least one selected checkbox or a note.

## Notifications

Shoutrrr receives a plain-text message:

```text
Service request for station1
Station: station1
What needs service: Toilet Paper, Cleaning
Note: Dispenser is jammed
```

Unchecked boxes are omitted. JSON objects stored by a custom schema are written as labeled lines.

## Templates

Override these files from the site template directory:

- `extension/service-station/form.html`
- `extension/service-station/admin_list.html`
- `extension/service-station/admin_form.html`
- `extension/service-station/admin_entry.html`
