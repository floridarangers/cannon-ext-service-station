# Service Station Extension

The Service Station extension collects restroom service requests for Cannon. Each public URL such as `/service/station1` or `/service/station2` is one facility. The station id is taken from that URL and submitted with the form as a hidden field.

## What It Provides

- A `service-station` page for `/service/{station}` routes.
- A JSON Forms schema for the public form, with a built-in restroom request.
- A configuration section for one Shoutrrr notification URL.
- Plain-text notifications. The message lists the station and form answers. It does not send the stored JSON.
- Stored requests in the extension admin.
- Embedded templates that can be overridden from the site template directory.

## Installation

When Cannon calls `/install`, the extension creates these tables and a default form:

- `service_station_settings`
- `service_station_forms`
- `service_station_entries`

## Configuration

Open the extension configuration area in Cannon and set `shoutrrr_url`.

The value is any service URL supported by [Shoutrrr](https://github.com/containrrr/shoutrrr), such as `slack://`, `discord://`, `smtp://`, or `logger://`.

Requests are still saved when the URL is empty.
