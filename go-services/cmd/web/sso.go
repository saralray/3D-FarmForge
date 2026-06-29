package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"printfarm/internal/saml"
)

// sso.go ports the SAML 2.0 SSO surface (the dashboard is the SP) and the shared
// HMAC grant/state hand-off (oauthGrant.js) from server/app.js. The cookieless
// SSO flow: /api/auth/saml/start → IdP → /api/auth/saml/acs (verifies the signed
// assertion, mints an HMAC auth grant) → browser lands on /login?oauth_grant=… →
// POST /api/auth/verify exchanges the grant for a real session cookie.

const (
	oauthSigningSecretKey = "oauth_signing_secret"
	samlSettingsKey       = "saml_sso"
	stateTTL              = 10 * 60 * 1000 // ms
	grantTTL              = 2 * 60 * 1000  // ms
)

var samlAllowedRoles = map[string]bool{"admin": true, "operator": true, "viewer": true, "student": true}

// ── HMAC state / grant tokens (port of oauthGrant.js) ────────────────────────

func ssoSign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func ssoEncode(secret string, data ojson) string {
	payload := base64.RawURLEncoding.EncodeToString(marshalJSON(data))
	return payload + "." + ssoSign(secret, payload)
}

// ssoDecode verifies the signature + expiry and returns the payload, or nil.
func ssoDecode(secret, token string) map[string]any {
	if secret == "" || token == "" {
		return nil
	}
	sep := strings.IndexByte(token, '.')
	if sep == -1 {
		return nil
	}
	payload, signature := token[:sep], token[sep+1:]
	expected := ssoSign(secret, payload)
	if len(signature) != len(expected) || !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	exp, ok := data["exp"].(float64)
	if !ok || float64(time.Now().UnixMilli()) > exp {
		return nil
	}
	return data
}

func signState(secret string, n, p string) string {
	return ssoEncode(secret, ojson{
		{"n", n}, {"p", p}, {"kind", "state"}, {"exp", time.Now().UnixMilli() + stateTTL},
	})
}

func verifyState(secret, token string) map[string]any {
	data := ssoDecode(secret, token)
	if data == nil {
		return nil
	}
	if kind, _ := data["kind"].(string); kind != "state" {
		return nil
	}
	return data
}

func mintAuthGrant(secret, provider, sub, email, name, role string) string {
	return ssoEncode(secret, ojson{
		{"kind", "grant"}, {"provider", provider}, {"sub", sub}, {"email", email},
		{"name", name}, {"role", role}, {"exp", time.Now().UnixMilli() + grantTTL},
	})
}

type authGrant struct {
	provider, sub, email, name, role string
}

func verifyAuthGrant(secret, token string) *authGrant {
	data := ssoDecode(secret, token)
	if data == nil {
		return nil
	}
	if kind, _ := data["kind"].(string); kind != "grant" {
		return nil
	}
	email, _ := data["email"].(string)
	if email == "" {
		return nil
	}
	g := &authGrant{email: email, provider: "google", sub: email, name: email, role: "student"}
	if v, ok := data["provider"].(string); ok {
		g.provider = v
	}
	if v, ok := data["sub"].(string); ok {
		g.sub = v
	}
	if v, ok := data["name"].(string); ok {
		g.name = v
	}
	if v, ok := data["role"].(string); ok {
		g.role = v
	}
	return g
}

// ── config getters ───────────────────────────────────────────────────────────

func getOAuthSigningSecret(ctx context.Context) (string, error) {
	raw, err := getAppSetting(ctx, oauthSigningSecretKey)
	if err != nil {
		return "", err
	}
	if s := storedString(decodeStored(raw), "secret"); len(s) >= 32 {
		return s, nil
	}
	secret, err := randomBase64URL(32)
	if err != nil {
		return "", err
	}
	if err := setAppSetting(ctx, oauthSigningSecretKey, map[string]any{"secret": secret}); err != nil {
		return "", err
	}
	return secret, nil
}

func resolvePublicOrigin(req *http.Request) string {
	proto := strings.TrimSpace(firstCSV(req.Header.Get("X-Forwarded-Proto")))
	if proto == "" {
		proto = "http"
	}
	host := strings.TrimSpace(firstCSV(req.Header.Get("X-Forwarded-Host")))
	if host == "" {
		host = strings.TrimSpace(firstCSV(req.Host))
	}
	if host == "" {
		host = "localhost"
	}
	return proto + "://" + host
}

func firstCSV(s string) string {
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[:i]
	}
	return s
}

func defaultSamlSpEntityID(req *http.Request) string {
	return resolvePublicOrigin(req) + "/api/auth/saml/metadata"
}
func defaultSamlAcsURL(req *http.Request) string {
	return resolvePublicOrigin(req) + "/api/auth/saml/acs"
}

type samlConfig struct {
	enabled            bool
	idpEntityID        string
	idpSsoURL          string
	idpCertificate     string
	spEntityID         string
	acsURL             string
	autoProvisionUsers bool
	updatedAt          *string
}

func getSamlConfig(ctx context.Context) (samlConfig, error) {
	raw, err := getAppSetting(ctx, samlSettingsKey)
	if err != nil {
		return samlConfig{}, err
	}
	m := decodeStored(raw)
	enabled, _ := m["enabled"].(bool)
	auto, _ := m["autoProvisionUsers"].(bool)
	cfg := samlConfig{
		enabled:            enabled,
		idpEntityID:        strings.TrimSpace(storedString(m, "idpEntityId")),
		idpSsoURL:          strings.TrimSpace(storedString(m, "idpSsoUrl")),
		idpCertificate:     strings.TrimSpace(storedString(m, "idpCertificate")),
		spEntityID:         strings.TrimSpace(storedString(m, "spEntityId")),
		acsURL:             strings.TrimSpace(storedString(m, "acsUrl")),
		autoProvisionUsers: auto,
	}
	if v, ok := m["updatedAt"].(string); ok {
		cfg.updatedAt = &v
	}
	return cfg, nil
}

func isSamlConfigured(c samlConfig) bool {
	return c.enabled && c.idpSsoURL != "" && c.idpCertificate != ""
}

func resolveSamlEndpoints(c samlConfig, req *http.Request) (string, string) {
	spEntityID := c.spEntityID
	if spEntityID == "" {
		spEntityID = defaultSamlSpEntityID(req)
	}
	acsURL := c.acsURL
	if acsURL == "" {
		acsURL = defaultSamlAcsURL(req)
	}
	return spEntityID, acsURL
}

func normalizeSamlRole(role string) string {
	if samlAllowedRoles[role] {
		return role
	}
	return "student"
}

// normalizeSamlRoleForNewUser caps the role an IdP can assert for a user that
// is not yet in the staff list (auto-provisioned). H-6 FIX: an IdP-asserted
// "admin" role must not grant admin access to an unknown account — only users
// already in the staff list with an admin role get it. New users land on
// "student" (read-only) or at most "operator" if the IdP explicitly claims it.
func normalizeSamlRoleForNewUser(role string) string {
	switch role {
	case "operator":
		return "operator"
	default:
		return "student"
	}
}

// ── routes ───────────────────────────────────────────────────────────────────

// handleSSORoutes covers /api/auth/verify, the SAML SP endpoints, and the SAML
// settings routes. Returns true once handled.
func handleSSORoutes(w http.ResponseWriter, req *http.Request) bool {
	ctx := req.Context()
	p := req.URL.Path
	m := req.Method

	switch {
	case p == "/api/auth/verify" && m == http.MethodPost:
		handleAuthVerify(ctx, w, req)
		return true
	case p == "/api/auth/saml/metadata" && m == http.MethodGet:
		handleSamlMetadata(ctx, w, req)
		return true
	case p == "/api/auth/saml/start" && m == http.MethodGet:
		handleSamlStart(ctx, w, req)
		return true
	case p == "/api/auth/saml/acs" && m == http.MethodPost:
		handleSamlAcs(ctx, w, req)
		return true
	case p == "/api/settings/saml" && (m == http.MethodGet || m == http.MethodPut):
		handleSamlSettings(ctx, w, req)
		return true
	case p == "/api/settings/saml/test" && m == http.MethodPost:
		handleSamlTest(ctx, w, req)
		return true
	}
	return false
}

func sendRedirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusFound)
}

func handleAuthVerify(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	secret, err := getOAuthSigningSecret(ctx)
	if err != nil {
		internalError(w, "getOAuthSigningSecret", err)
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	_ = readJSONBody(req, &body)
	grant := verifyAuthGrant(secret, body.Token)
	if grant == nil {
		sendJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid or expired sign-in"}, "")
		return
	}
	user := sessionUser{ID: grant.provider + ":" + grant.sub, Name: grant.name, Username: grant.email, Role: grant.role}
	if err := issueSession(ctx, w, req, user, true); err != nil {
		internalError(w, "issueSession", err)
		return
	}
	sendJSON(w, http.StatusOK, map[string]any{"user": userPayload{ID: user.ID, Name: user.Name, Username: user.Username, Role: user.Role}}, "")
}

func handleSamlMetadata(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	cfg, err := getSamlConfig(ctx)
	if err != nil {
		internalError(w, "getSamlConfig", err)
		return
	}
	spEntityID, acsURL := resolveSamlEndpoints(cfg, req)
	xml := saml.BuildSpMetadata(spEntityID, acsURL)
	w.Header().Set("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="sp-metadata.xml"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, xml)
}

func handleSamlStart(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	cfg, err := getSamlConfig(ctx)
	if err != nil {
		internalError(w, "getSamlConfig", err)
		return
	}
	if !isSamlConfigured(cfg) {
		sendRedirect(w, "/login?oauth_error=not_configured")
		return
	}
	spEntityID, acsURL := resolveSamlEndpoints(cfg, req)
	secret, err := getOAuthSigningSecret(ctx)
	if err != nil {
		internalError(w, "getOAuthSigningSecret", err)
		return
	}
	authURL, requestID, err := saml.BuildAuthnRequest(spEntityID, acsURL, cfg.idpSsoURL, "")
	if err != nil {
		internalError(w, "BuildAuthnRequest", err)
		return
	}
	relayState := signState(secret, requestID, "saml")
	u, err := url.Parse(authURL)
	if err != nil {
		internalError(w, "parse authURL", err)
		return
	}
	q := u.Query()
	q.Set("RelayState", relayState)
	u.RawQuery = q.Encode()
	sendRedirect(w, u.String())
}

func handleSamlAcs(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	cfg, err := getSamlConfig(ctx)
	if err != nil {
		internalError(w, "getSamlConfig", err)
		return
	}
	if !isSamlConfigured(cfg) {
		sendRedirect(w, "/login?oauth_error=not_configured")
		return
	}
	spEntityID, acsURL := resolveSamlEndpoints(cfg, req)
	secret, err := getOAuthSigningSecret(ctx)
	if err != nil {
		internalError(w, "getOAuthSigningSecret", err)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(req.Body, maxBodyBytes))
	if err != nil {
		sendRedirect(w, "/login?oauth_error=denied")
		return
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		sendRedirect(w, "/login?oauth_error=denied")
		return
	}
	samlResponseB64 := form.Get("SAMLResponse")
	relayState := form.Get("RelayState")
	relayData := verifyState(secret, relayState)
	expectedInResponseTo := ""
	if relayData != nil {
		if pv, _ := relayData["p"].(string); pv == "saml" {
			if nv, ok := relayData["n"].(string); ok {
				expectedInResponseTo = nv
			}
		}
	}

	identity, err := saml.ParseAndVerify(samlResponseB64, cfg.idpCertificate, spEntityID, acsURL, expectedInResponseTo)
	if err != nil {
		sendRedirect(w, "/login?oauth_error=saml_invalid")
		return
	}

	staffUsers, _ := readStaffUsers(ctx)
	var existing *staffUser
	for i := range staffUsers {
		if strings.EqualFold(staffUsers[i].Username, identity.Email) {
			existing = &staffUsers[i]
			break
		}
	}
	if existing == nil && !cfg.autoProvisionUsers {
		sendRedirect(w, "/login?oauth_error=saml_not_provisioned")
		return
	}
	var role string
	if existing != nil && userRoles[existing.Role] {
		// Known account: always use the stored role; IdP cannot escalate it.
		role = existing.Role
	} else {
		// H-6 FIX: auto-provisioned (new) user — cap IdP-asserted role to
		// "operator" at most; "admin" from the IdP is silently downgraded.
		role = normalizeSamlRoleForNewUser(normalizeSamlRole(identity.Role))
	}
	name := identity.Name
	if name == "" {
		name = identity.Email
	}
	grant := mintAuthGrant(secret, "saml", identity.Email, identity.Email, name, role)
	sendRedirect(w, "/login?oauth_grant="+url.QueryEscape(grant))
}

// samlSettingsResponse keeps Node's key order (config fields, then the four
// derived endpoints).
type samlSettingsResponse struct {
	Enabled             bool    `json:"enabled"`
	IdpEntityID         string  `json:"idpEntityId"`
	IdpSsoURL           string  `json:"idpSsoUrl"`
	IdpCertificate      string  `json:"idpCertificate"`
	SpEntityID          string  `json:"spEntityId"`
	AcsURL              string  `json:"acsUrl"`
	AutoProvisionUsers  bool    `json:"autoProvisionUsers"`
	UpdatedAt           *string `json:"updatedAt"`
	DefaultSpEntityID   string  `json:"defaultSpEntityId"`
	DefaultAcsURL       string  `json:"defaultAcsUrl"`
	EffectiveSpEntityID string  `json:"effectiveSpEntityId"`
	EffectiveAcsURL     string  `json:"effectiveAcsUrl"`
}

func samlSettingsBody(cfg samlConfig, req *http.Request) samlSettingsResponse {
	spEntityID, acsURL := resolveSamlEndpoints(cfg, req)
	return samlSettingsResponse{
		Enabled:             cfg.enabled,
		IdpEntityID:         cfg.idpEntityID,
		IdpSsoURL:           cfg.idpSsoURL,
		IdpCertificate:      cfg.idpCertificate,
		SpEntityID:          cfg.spEntityID,
		AcsURL:              cfg.acsURL,
		AutoProvisionUsers:  cfg.autoProvisionUsers,
		UpdatedAt:           cfg.updatedAt,
		DefaultSpEntityID:   defaultSamlSpEntityID(req),
		DefaultAcsURL:       defaultSamlAcsURL(req),
		EffectiveSpEntityID: spEntityID,
		EffectiveAcsURL:     acsURL,
	}
}

func handleSamlSettings(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodGet {
		cfg, err := getSamlConfig(ctx)
		if err != nil {
			internalError(w, "getSamlConfig", err)
			return
		}
		sendJSON(w, http.StatusOK, samlSettingsBody(cfg, req), "")
		return
	}

	body := decodeBodyMap(req)
	enabled, _ := body["enabled"].(bool)
	idpEntityID := strings.TrimSpace(storedString(body, "idpEntityId"))
	idpSsoURL := strings.TrimSpace(storedString(body, "idpSsoUrl"))
	idpCertificate := strings.TrimSpace(storedString(body, "idpCertificate"))
	spEntityID := strings.TrimSpace(storedString(body, "spEntityId"))
	acsURL := strings.TrimSpace(storedString(body, "acsUrl"))
	autoProvisionUsers, _ := body["autoProvisionUsers"].(bool)

	for _, pair := range []struct{ label, value string }{
		{"IdP SSO URL", idpSsoURL}, {"SP entity ID", spEntityID}, {"ACS URL", acsURL},
	} {
		if pair.value != "" && !saml.IsValidHTTPURL(pair.value) {
			badRequest(w, pair.label+" must be a valid http(s) URL")
			return
		}
	}
	if idpCertificate != "" && !saml.IsValidCertificate(idpCertificate) {
		badRequest(w, "IdP certificate is not a valid X.509 PEM certificate")
		return
	}
	if enabled && (idpSsoURL == "" || idpCertificate == "") {
		badRequest(w, "An IdP SSO URL and certificate are required to enable SAML SSO")
		return
	}

	now := time.Now()
	updatedAt := jsISO(&now)
	if err := setAppSetting(ctx, samlSettingsKey, ojson{
		{"enabled", enabled}, {"idpEntityId", idpEntityID}, {"idpSsoUrl", idpSsoURL},
		{"idpCertificate", idpCertificate}, {"spEntityId", spEntityID}, {"acsUrl", acsURL},
		{"autoProvisionUsers", autoProvisionUsers}, {"updatedAt", *updatedAt},
	}); err != nil {
		internalError(w, "setAppSetting saml", err)
		return
	}
	details := json.RawMessage(marshalJSON(ojson{{"enabled", enabled}, {"autoProvisionUsers", autoProvisionUsers}}))
	target := "saml_sso"
	_ = recordAuditLog(ctx, auditEntry{Action: "settings.saml.update", Target: &target, Details: details, Source: "web", IP: getClientIP(req)})

	saved, err := getSamlConfig(ctx)
	if err != nil {
		internalError(w, "getSamlConfig", err)
		return
	}
	sendJSON(w, http.StatusOK, samlSettingsBody(saved, req), "")
}

type samlCheck struct {
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

func handleSamlTest(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	stored, err := getSamlConfig(ctx)
	if err != nil {
		internalError(w, "getSamlConfig", err)
		return
	}
	body := decodeBodyMap(req)
	idpSsoURL := stored.idpSsoURL
	if v := strings.TrimSpace(storedString(body, "idpSsoUrl")); v != "" {
		idpSsoURL = v
	}
	idpCertificate := stored.idpCertificate
	if v := strings.TrimSpace(storedString(body, "idpCertificate")); v != "" {
		idpCertificate = v
	}

	urlOK := saml.IsValidHTTPURL(idpSsoURL)
	checks := []samlCheck{
		{Label: "IdP SSO URL is a valid http(s) URL", OK: urlOK},
		{Label: "IdP certificate is a valid X.509 certificate", OK: saml.IsValidCertificate(idpCertificate)},
	}
	if urlOK {
		// H-3 FIX: use an SSRF-safe transport that blocks requests to
		// private/loopback/link-local IPs so an admin cannot probe internal
		// services via the SAML test endpoint (see ssrf.go + redis.go).
		reachable := false
		detail := ""
		client := &http.Client{
			Timeout:       5 * time.Second,
			Transport:     samlProbeTransport(),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		probe, perr := client.Get(idpSsoURL)
		if perr != nil {
			detail = perr.Error()
		} else {
			reachable = true
			detail = "HTTP " + itoa(probe.StatusCode)
			_ = probe.Body.Close()
		}
		checks = append(checks, samlCheck{Label: "IdP SSO URL is reachable", OK: reachable, Detail: detail})
	}

	ok := true
	for _, c := range checks {
		if !c.OK {
			ok = false
		}
	}
	sendJSON(w, http.StatusOK, samlTestResponse{OK: ok, Checks: checks}, "")
}

// samlTestResponse keeps Node's { ok, checks } key order.
type samlTestResponse struct {
	OK     bool        `json:"ok"`
	Checks []samlCheck `json:"checks"`
}
