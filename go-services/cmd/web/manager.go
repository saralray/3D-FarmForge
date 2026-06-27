package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"

	"printfarm/internal/pwcrypto"
)

// manager.go ports the manager-access-request workflow (server/app.js
// /api/manager/*), the admin Discord-webhook and slicer-key CRUD, and the
// /api/version build-id probe. The authorization gate has already classified
// these (public intake + status poll; admin list/approve/deny/delete/CRUD).

// ── build id (/api/version) ──────────────────────────────────────────────────

var (
	buildIDOnce sync.Once
	buildIDVal  = "dev"
)

// buildID mirrors Node's BUILD_ID: sha256(dist/index.html)[:16], or "dev" when
// the file is absent. Computed once, lazily, like the Node startup hook.
func buildID() string {
	buildIDOnce.Do(func() {
		raw, err := os.ReadFile(filepath.Join(distDir, "index.html"))
		if err != nil {
			return
		}
		sum := sha256.Sum256(raw)
		buildIDVal = hex.EncodeToString(sum[:])[:16]
	})
	return buildIDVal
}

// ── manager access requests ──────────────────────────────────────────────────

func handleManagerRoutes(ctx context.Context, w http.ResponseWriter, req *http.Request) bool {
	p := req.URL.Path
	m := req.Method

	// POST /api/manager/request — public create (+ CORS preflight).
	if p == "/api/manager/request" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if m == http.MethodOptions {
			sendEmpty(w, http.StatusNoContent)
			return true
		}
		if m == http.MethodPost {
			body := decodeBodyMap(req)
			name, _ := body["name"].(string)
			if strings.TrimSpace(name) == "" {
				badRequest(w, "name is required")
				return true
			}
			id := uuid.NewString()
			if err := createManagerRequest(ctx, id, strings.TrimSpace(name), trimmedPtr(body["description"])); err != nil {
				internalError(w, "createManagerRequest", err)
				return true
			}
			sendJSON(w, http.StatusCreated, ojson{{"id", id}}, "")
			return true
		}
		// Other methods: headers set, fall through (matches Node).
		return false
	}

	// GET /api/manager/requests — admin list.
	if p == "/api/manager/requests" && m == http.MethodGet {
		data, err := listManagerRequestsJSON(ctx)
		return sendStoreJSON(w, data, err)
	}

	// GET /api/manager/requests/:id/status — public status poll (+ CORS), reveals
	// the minted key once.
	if strings.HasPrefix(p, "/api/manager/requests/") && strings.HasSuffix(p, "/status") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if m == http.MethodOptions {
			sendEmpty(w, http.StatusNoContent)
			return true
		}
		if m == http.MethodGet {
			id := decodePathSegment(p, "/api/manager/requests/", "/status")
			mgr, err := getManagerRequest(ctx, id)
			if err != nil {
				internalError(w, "getManagerRequest", err)
				return true
			}
			if mgr == nil {
				sendJSON(w, http.StatusNotFound, map[string]any{"error": "Request not found"}, "")
				return true
			}
			payload := ojson{{"id", mgr.ID}, {"status", mgr.Status}}
			if mgr.Status == "approved" && mgr.KeySecret != nil && *mgr.KeySecret != "" {
				payload = append(payload, ojField{"key", *mgr.KeySecret})
				if err := clearManagerRequestKeySecret(ctx, id); err != nil {
					internalError(w, "clearManagerRequestKeySecret", err)
					return true
				}
			}
			sendJSON(w, http.StatusOK, payload, "")
			return true
		}
		return false
	}

	// POST /api/manager/requests/:id/approve — admin; mint key + approve.
	if strings.HasPrefix(p, "/api/manager/requests/") && strings.HasSuffix(p, "/approve") && m == http.MethodPost {
		id := decodePathSegment(p, "/api/manager/requests/", "/approve")
		mgr, err := getManagerRequest(ctx, id)
		if err != nil {
			internalError(w, "getManagerRequest", err)
			return true
		}
		if mgr == nil {
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "Request not found"}, "")
			return true
		}
		if mgr.Status != "pending" {
			badRequest(w, "Request is not pending")
			return true
		}
		key, err := randomBase64URL(24)
		if err != nil {
			internalError(w, "randomBase64URL", err)
			return true
		}
		keyID := uuid.NewString()
		if err := createSlicerApiKey(ctx, keyID, "Manager: "+mgr.Name, pwcrypto.Hash(key), key[:8],
			[]string{"printfarm_manage"}, nil); err != nil {
			internalError(w, "createSlicerApiKey", err)
			return true
		}
		if err := approveManagerRequest(ctx, id, keyID, key); err != nil {
			internalError(w, "approveManagerRequest", err)
			return true
		}
		sendJSON(w, http.StatusOK, ojson{{"ok", true}}, "")
		return true
	}

	// POST /api/manager/requests/:id/deny — admin.
	if strings.HasPrefix(p, "/api/manager/requests/") && strings.HasSuffix(p, "/deny") && m == http.MethodPost {
		id := decodePathSegment(p, "/api/manager/requests/", "/deny")
		mgr, err := getManagerRequest(ctx, id)
		if err != nil {
			internalError(w, "getManagerRequest", err)
			return true
		}
		if mgr == nil {
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "Request not found"}, "")
			return true
		}
		if mgr.Status != "pending" {
			badRequest(w, "Request is not pending")
			return true
		}
		if err := denyManagerRequest(ctx, id); err != nil {
			internalError(w, "denyManagerRequest", err)
			return true
		}
		sendJSON(w, http.StatusOK, ojson{{"ok", true}}, "")
		return true
	}

	// DELETE /api/manager/requests/:id — admin; revoke key + remove.
	if strings.HasPrefix(p, "/api/manager/requests/") && m == http.MethodDelete {
		id := decodePathSegment(p, "/api/manager/requests/", "")
		mgr, err := getManagerRequest(ctx, id)
		if err != nil {
			internalError(w, "getManagerRequest", err)
			return true
		}
		if mgr == nil {
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "Request not found"}, "")
			return true
		}
		if mgr.APIKeyID != nil {
			if err := deleteSlicerApiKey(ctx, *mgr.APIKeyID); err != nil {
				internalError(w, "deleteSlicerApiKey", err)
				return true
			}
		}
		if err := deleteManagerRequest(ctx, id); err != nil {
			internalError(w, "deleteManagerRequest", err)
			return true
		}
		sendEmpty(w, http.StatusNoContent)
		return true
	}

	return false
}

// ── Discord webhook CRUD (frontend, admin) ───────────────────────────────────

func handleNotificationsRoutes(ctx context.Context, w http.ResponseWriter, req *http.Request) bool {
	p := req.URL.Path
	m := req.Method

	if p == "/api/notifications/discord-webhooks" {
		if m == http.MethodGet {
			data, err := listDiscordWebhooksJSON(ctx)
			return sendStoreJSON(w, data, err)
		}
		if m == http.MethodPost {
			raw, _ := rawBody(req)
			if err := createDiscordWebhook(ctx, raw); err != nil {
				internalError(w, "createDiscordWebhook", err)
				return true
			}
			sendEmpty(w, http.StatusNoContent)
			return true
		}
		return false
	}

	if strings.HasPrefix(p, "/api/notifications/discord-webhooks/") && m == http.MethodDelete {
		id := decodePathSegment(p, "/api/notifications/discord-webhooks/", "")
		if err := deleteDiscordWebhook(ctx, id); err != nil {
			internalError(w, "deleteDiscordWebhook", err)
			return true
		}
		sendEmpty(w, http.StatusNoContent)
		return true
	}

	return false
}

// ── slicer-key CRUD (frontend, admin) ────────────────────────────────────────

func handleSlicerKeysRoutes(ctx context.Context, w http.ResponseWriter, req *http.Request) bool {
	p := req.URL.Path
	m := req.Method

	if p == "/api/slicer-keys" {
		if m == http.MethodGet {
			data, err := listSlicerApiKeysJSON(ctx)
			return sendStoreJSON(w, data, err)
		}
		if m == http.MethodPost {
			body := decodeBodyMap(req)
			name, _ := body["name"].(string)
			if strings.TrimSpace(name) == "" {
				badRequest(w, "name is required")
				return true
			}
			scopes := normalizeKeyPermissions(stringSlice(body["permissions"]))
			if len(scopes) == 0 {
				badRequest(w, "permissions must include at least one of: "+strings.Join(slicerKeyPermissions, ", "))
				return true
			}
			key, err := randomBase64URL(24)
			if err != nil {
				internalError(w, "randomBase64URL", err)
				return true
			}
			id := uuid.NewString()
			trimmed := strings.TrimSpace(name)
			if err := createSlicerApiKey(ctx, id, trimmed, pwcrypto.Hash(key), key[:8], scopes, nil); err != nil {
				internalError(w, "createSlicerApiKey", err)
				return true
			}
			sendJSON(w, http.StatusCreated, ojson{
				{"id", id}, {"name", trimmed}, {"key", key}, {"permissions", scopes},
			}, "")
			return true
		}
		return false
	}

	if strings.HasPrefix(p, "/api/slicer-keys/") && m == http.MethodDelete {
		id := decodePathSegment(p, "/api/slicer-keys/", "")
		if err := deleteSlicerApiKey(ctx, id); err != nil {
			internalError(w, "deleteSlicerApiKey", err)
			return true
		}
		sendEmpty(w, http.StatusNoContent)
		return true
	}

	return false
}

// trimmedPtr mirrors `typeof x === 'string' ? x.trim() || null : null`.
func trimmedPtr(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if t := strings.TrimSpace(s); t != "" {
		return &t
	}
	return nil
}
