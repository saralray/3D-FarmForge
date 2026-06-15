# STEM Lab Print Farm — Data API (`/api/v1`)

A versioned, API-key-gated HTTP API over the print farm's data. It is served by
the `web` service (`handleDataApi` in `server/app.js`) and is **entirely
separate** from the cookieless frontend `/api/*` endpoints the dashboard uses —
those stay unauthenticated and unchanged. This `/api/v1` namespace is for
external integrations, scripts, and dashboards.

- **Base URL:** `http://<host>:<HTTP_PORT>/api/v1` (default port `8080`, served
  through nginx)
- **Format:** JSON request and response bodies (`Content-Type: application/json`)
- **Auth:** required on every request (see below)

---

## Authentication

Every request must present a valid API key. Keys are the **same named keys**
minted in **Settings → Slicer Upload** (stored in `slicer_api_keys` as a sha256
hash; the plaintext is shown only once at creation).

Pass the key either way:

```http
X-Api-Key: <your-key>
```
```http
Authorization: Bearer <your-key>
```

- Any valid key grants **full read/write** to every resource.
- Each request stamps the key's `last_used_at`.
- Every **mutation** (POST/PUT/DELETE) is recorded in the audit log with
  `source: "api"` and actor `api:<key name>`.
- A missing or invalid key returns **`401 Unauthorized`**.

> ⚠️ Because slicer-upload keys are reused here, any slicer key also grants full
> data access. Scope and revoke keys accordingly.

### Example

```bash
curl -H "X-Api-Key: $KEY" http://localhost:8080/api/v1/printers
```

---

## Conventions

| Status | Meaning |
|--------|---------|
| `200`  | OK, body returned |
| `201`  | Created |
| `204`  | OK, no body |
| `400`  | Bad request (missing/invalid fields) |
| `401`  | Missing or invalid API key |
| `404`  | Unknown resource or record |
| `405`  | Method not allowed for that path |
| `500`  | Server / database error |

Connection details (IP, API key header, serial) **are** returned by this API —
the key is the guard, so unlike the public-viewer mode nothing is redacted.

---

## Discovery

### `GET /api/v1`
Lists the available resources.

```json
{
  "version": "v1",
  "resources": ["printers", "queue", "analytics", "notifications", "slicer-keys", "audit-logs", "settings"]
}
```

---

## Resources

### Printers — `/api/v1/printers`

| Method & path | Description |
|---------------|-------------|
| `GET /printers` | List all printers (full detail). |
| `GET /printers/:id` | Fetch one printer; `404` if not found. |
| `POST /printers` | Create or update a printer. Body must include `id`. Returns the saved record. |
| `DELETE /printers/:id` | Delete a printer. |
| `POST /printers/:id/command` | Send a Bambu MQTT command (pause/resume/cancel, temps, fans, etc.). |
| `GET /printers/:id/camera/snapshot` | A single JPEG frame from the printer's webcam. |
| `GET /printers/:id/camera/stream` | Live MJPEG stream where supported, else a single JPEG. |
| `GET /printers/:id/camera/health` | Live-view supervisor status (frame freshness, viewers, restarts). |

**Upsert body (example):**
```json
{
  "id": "printer-1",
  "name": "Bambu A1 #1",
  "model": "A1 Mini",
  "profile": "bambulab_a1_mini",
  "ipAddress": "192.168.1.50",
  "apiKeyHeader": "<lan-access-code>",
  "serial": "0309XXXXXXXXXXX"
}
```

**Command body (example):**
```json
{ "command": "pause" }
```
Other accepted fields: `heater`, `target`, `nozzleIndex`, `gcode`, `trayId`,
`fanPort`, `speed`, `modeId`, `submode`.

#### Webcam

The camera endpoints return image data, **not** JSON:

- `GET /printers/:id/camera/snapshot` → `image/jpeg` (one frame).
- `GET /printers/:id/camera/stream` → `multipart/x-mixed-replace` MJPEG for
  live-capable profiles (Snapmaker U1, Bambu H2 series); other profiles
  (e.g. Bambu A1 Mini, which is snapshot-only) return a single JPEG.
- `GET /printers/:id/camera/health` → JSON supervisor status
  (`status`, `online`, `viewers`, `lastFrameAgeMs`, `restarts`, `lastError`).

They route through the same internal webcam proxy as the dashboard, so the
printer must have its camera reachable (and for Bambu, **LAN Mode Liveview**
enabled). Drop a snapshot straight into an `<img>`:

```html
<img src="http://localhost:8080/api/v1/printers/printer-1/camera/stream" />
```

> Note: an `<img>`/`<video>` tag cannot send an `X-Api-Key` header. For
> browser-embedded streams, either use the unauthenticated friendly route
> `/webcam/<id>` (no key), or proxy the `/api/v1` request server-side and
> forward the key.

---

### Queue — `/api/v1/queue`

GET returns the **stored** queue jobs. It does **not** trigger a Google Sheet
sync — that behavior lives on the frontend `/api/queue` path only.

| Method & path | Description |
|---------------|-------------|
| `GET /queue` | List stored queue jobs. |
| `POST /queue` | Upsert jobs. Body is an array, or `{ "jobs": [...] }`. Returns `{ "added": [...] }`. |
| `POST /queue/reset` | Clear `printed_status` for all non-deleted jobs. |
| `POST /queue/:id/printed` | Mark a job printed. |
| `DELETE /queue/:id` | Soft-delete a job (sets `deleted_at`). |

---

### Analytics — `/api/v1/analytics`

| Method & path | Description |
|---------------|-------------|
| `GET /analytics?days=7` | Daily analytics rollups. `days` defaults to `7`. |
| `POST /analytics/reset` | Reset the daily analytics. |

---

### Notifications (Discord webhooks) — `/api/v1/notifications`

| Method & path | Description |
|---------------|-------------|
| `GET /notifications` | List configured Discord webhooks. |
| `POST /notifications` | Create/update a webhook. Generates an `id` if omitted; returns `{ "id": ... }`. |
| `DELETE /notifications/:id` | Delete a webhook. |

**Body (example):**
```json
{
  "name": "Build Room",
  "webhookUrl": "https://discord.com/api/webhooks/...",
  "events": ["queue_added", "print_done"],
  "enabled": true
}
```

---

### Slicer API keys — `/api/v1/slicer-keys`

| Method & path | Description |
|---------------|-------------|
| `GET /slicer-keys` | List keys (metadata only — never the secret). |
| `POST /slicer-keys` | Mint a key. Body `{ "name": "..." }`. **Plaintext key returned once** in the response. |
| `DELETE /slicer-keys/:id` | Revoke a key. |

**Create response (key shown only here):**
```json
{ "id": "uuid", "name": "Orca Slicer", "key": "plaintext-key-shown-once" }
```

---

### Audit logs — `/api/v1/audit-logs`

| Method & path | Description |
|---------------|-------------|
| `GET /audit-logs?limit=200` | Most recent entries first. `limit` clamped to 1–1000. |
| `POST /audit-logs` | Append an entry. Body requires `action`; optional `target`, `details`. |

---

### Settings (app_settings key/value) — `/api/v1/settings`

| Method & path | Description |
|---------------|-------------|
| `GET /settings/:key` | Read a setting. Returns `{ "key": ..., "value": ... }` (`value` is `null` if unset). |
| `PUT /settings/:key` | Write a setting. Body `{ "value": <any> }`, or the raw value as the whole body. |

`POST` is accepted as an alias for `PUT`.

---

## Quick reference (curl)

```bash
KEY="your-api-key"
BASE="http://localhost:8080/api/v1"

# discovery
curl -H "X-Api-Key: $KEY" "$BASE"

# printers
curl -H "X-Api-Key: $KEY" "$BASE/printers"
curl -H "X-Api-Key: $KEY" "$BASE/printers/printer-1"
curl -H "X-Api-Key: $KEY" -X POST "$BASE/printers" \
     -H "Content-Type: application/json" \
     -d '{"id":"printer-1","name":"A1 #1","profile":"bambulab_a1_mini"}'
curl -H "X-Api-Key: $KEY" -X POST "$BASE/printers/printer-1/command" \
     -H "Content-Type: application/json" -d '{"command":"pause"}'
curl -H "X-Api-Key: $KEY" -X DELETE "$BASE/printers/printer-1"

# webcam
curl -H "X-Api-Key: $KEY" "$BASE/printers/printer-1/camera/snapshot" -o frame.jpg
curl -H "X-Api-Key: $KEY" "$BASE/printers/printer-1/camera/health"

# queue / analytics / settings
curl -H "X-Api-Key: $KEY" "$BASE/queue"
curl -H "X-Api-Key: $KEY" "$BASE/analytics?days=30"
curl -H "X-Api-Key: $KEY" -X PUT "$BASE/settings/printer_card_layout" \
     -H "Content-Type: application/json" -d '{"value":{"foo":"bar"}}'
```
