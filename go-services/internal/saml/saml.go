// Package saml is the SAML 2.0 Service Provider (SP) half of the dashboard's SSO,
// ported from server/samlSp.js. It builds AuthnRequests (HTTP-Redirect binding),
// verifies the IdP's signed SAMLResponse (HTTP-POST binding) against the
// configured certificate, and emits SP metadata.
//
// The signature is verified against the *configured* certificate only and the
// attributes are read from the specific Assertion node the signature covers,
// which together defend against XML signature-wrapping.
package saml

import (
	"bytes"
	"compress/flate"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

const (
	protocolNS  = "urn:oasis:names:tc:SAML:2.0:protocol"
	assertionNS = "urn:oasis:names:tc:SAML:2.0:assertion"
	dsigNS      = "http://www.w3.org/2000/09/xmldsig#"
	bindingPost = "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"
	nameIDEmail = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"
	statusOK    = "urn:oasis:names:tc:SAML:2.0:status:Success"
)

// clockSkew mirrors CLOCK_SKEW_MS (3 minutes).
const clockSkew = 3 * time.Minute

// Error is the SAML-specific failure type (mirrors SamlError); the ACS treats it
// as an invalid-response redirect.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }
func newErr(m string) *Error   { return &Error{Msg: m} }

// Identity is the verified subject pulled from an assertion.
type Identity struct {
	Username string
	Email    string
	Name     string
	Role     string
}

// ── validation helpers ───────────────────────────────────────────────────────

func IsValidHTTPURL(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	u, err := url.Parse(v)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

var certBodyStrip = regexp.MustCompile(`(?s)-----BEGIN CERTIFICATE-----|-----END CERTIFICATE-----`)
var whitespace = regexp.MustCompile(`\s+`)
var base64Only = regexp.MustCompile(`^[A-Za-z0-9+/=]+$`)

func certBody(certificate string) string {
	s := certBodyStrip.ReplaceAllString(certificate, "")
	return whitespace.ReplaceAllString(s, "")
}

// NormalizeCertificatePEM wraps a bare base64 cert body in PEM armor, or returns
// an already-PEM cert as-is.
func NormalizeCertificatePEM(certificate string) string {
	raw := strings.TrimSpace(certificate)
	if strings.Contains(raw, "-----BEGIN CERTIFICATE-----") {
		return raw
	}
	body := certBody(raw)
	if body == "" {
		return ""
	}
	var lines []string
	for i := 0; i < len(body); i += 64 {
		end := i + 64
		if end > len(body) {
			end = len(body)
		}
		lines = append(lines, body[i:end])
	}
	return "-----BEGIN CERTIFICATE-----\n" + strings.Join(lines, "\n") + "\n-----END CERTIFICATE-----"
}

func IsValidCertificate(certificate string) bool {
	body := certBody(certificate)
	if len(body) < 64 || !base64Only.MatchString(body) {
		return false
	}
	der, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return false
	}
	return len(der) > 16 && der[0] == 0x30
}

// ── AuthnRequest (SP → IdP, HTTP-Redirect binding) ───────────────────────────

func uid() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "_" + hex.EncodeToString(b)
}

// nowIso mirrors nowIso(): ISO 8601 with no fractional seconds.
func nowIso() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func escapeXML(value string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return r.Replace(value)
}

// BuildAuthnRequest returns the IdP redirect URL carrying a DEFLATE+base64
// AuthnRequest, plus the request id (to bind into the signed RelayState).
func BuildAuthnRequest(spEntityID, acsURL, idpSsoURL, relayState string) (string, string, error) {
	requestID := uid()
	xml := `<samlp:AuthnRequest xmlns:samlp="` + protocolNS + `" xmlns:saml="` + assertionNS + `"` +
		` ID="` + requestID + `" Version="2.0" IssueInstant="` + nowIso() + `"` +
		` Destination="` + escapeXML(idpSsoURL) + `"` +
		` AssertionConsumerServiceURL="` + escapeXML(acsURL) + `"` +
		` ProtocolBinding="` + bindingPost + `">` +
		`<saml:Issuer>` + escapeXML(spEntityID) + `</saml:Issuer>` +
		`</samlp:AuthnRequest>`

	deflated, err := deflateRawBase64([]byte(xml))
	if err != nil {
		return "", "", err
	}
	u, err := url.Parse(idpSsoURL)
	if err != nil {
		return "", "", err
	}
	q := u.Query()
	q.Set("SAMLRequest", deflated)
	if relayState != "" {
		q.Set("RelayState", relayState)
	}
	u.RawQuery = q.Encode()
	return u.String(), requestID, nil
}

func deflateRawBase64(data []byte) (string, error) {
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// ── SAMLResponse parse + verify ──────────────────────────────────────────────

func directChildrenNS(parent *etree.Element, ns, local string) []*etree.Element {
	var out []*etree.Element
	for _, c := range parent.ChildElements() {
		if c.Tag == local && c.NamespaceURI() == ns {
			out = append(out, c)
		}
	}
	return out
}

func firstChildNS(parent *etree.Element, ns, local string) *etree.Element {
	cs := directChildrenNS(parent, ns, local)
	if len(cs) == 0 {
		return nil
	}
	return cs[0]
}

// firstChildPath walks a chain of namespaced children (all in assertionNS).
func firstChildPath(parent *etree.Element, locals ...string) *etree.Element {
	cur := parent
	for _, l := range locals {
		if cur == nil {
			return nil
		}
		cur = firstChildNS(cur, assertionNS, l)
	}
	return cur
}

// textContent mirrors DOM textContent.trim(): concatenated descendant char data.
func textContent(e *etree.Element) string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*etree.Element)
	walk = func(el *etree.Element) {
		for _, tok := range el.Child {
			switch t := tok.(type) {
			case *etree.CharData:
				b.WriteString(t.Data)
			case *etree.Element:
				walk(t)
			}
		}
	}
	walk(e)
	return strings.TrimSpace(b.String())
}

func verifyAssertionSignature(assertion *etree.Element, certPEM string) bool {
	// Exactly one Signature that is a direct child of the assertion.
	sigs := directChildrenNS(assertion, dsigNS, "Signature")
	if len(sigs) != 1 {
		return false
	}
	block := NormalizeCertificatePEM(certPEM)
	cert, err := parsePEMCertificate(block)
	if err != nil {
		return false
	}
	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	ctx := dsig.NewDefaultValidationContext(store)
	// goxmldsig validates the detached element in isolation, so namespace prefixes
	// declared on an ancestor (the IdP commonly puts xmlns:saml/samlp on the
	// Response, not the Assertion) would be "undeclared". xml-crypto's exclusive
	// c14n pulls them from ancestors; reproduce that by making the assertion
	// self-contained first. goxmldsig also enforces that the signed reference points
	// at the element we hand it, which with the configured-cert-only store
	// reproduces the wrapping defense.
	if _, err := ctx.Validate(selfContainedAssertion(assertion)); err != nil {
		return false
	}
	return true
}

// selfContainedAssertion copies the assertion and injects any xmlns declarations
// inherited from its ancestors (nearest declaration wins), so exclusive-c14n sees
// the same namespace context the signature was computed over.
func selfContainedAssertion(assertion *etree.Element) *etree.Element {
	cp := assertion.Copy()
	declared := map[string]bool{}
	for _, a := range cp.Attr {
		if a.Space == "xmlns" {
			declared[a.Key] = true
		} else if a.Space == "" && a.Key == "xmlns" {
			declared["xmlns"] = true
		}
	}
	for anc := assertion.Parent(); anc != nil; anc = anc.Parent() {
		for _, a := range anc.Attr {
			if a.Space == "xmlns" && !declared[a.Key] {
				cp.CreateAttr("xmlns:"+a.Key, a.Value)
				declared[a.Key] = true
			} else if a.Space == "" && a.Key == "xmlns" && !declared["xmlns"] {
				cp.CreateAttr("xmlns", a.Value)
				declared["xmlns"] = true
			}
		}
	}
	return cp
}

func parsePEMCertificate(pemStr string) (*x509.Certificate, error) {
	body := certBody(pemStr)
	der, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}

func withinWindow(notBefore, notOnOrAfter string, now time.Time) bool {
	if notBefore != "" {
		if start, err := time.Parse(time.RFC3339, notBefore); err == nil {
			if now.Add(clockSkew).Before(start) {
				return false
			}
		}
	}
	if notOnOrAfter != "" {
		if end, err := time.Parse(time.RFC3339, notOnOrAfter); err == nil {
			if !now.Add(-clockSkew).Before(end) {
				return false
			}
		}
	}
	return true
}

var hasTag = regexp.MustCompile(`<.+>`)

// ParseAndVerify validates a base64 SAMLResponse and returns the subject, or an
// *Error with a short reason on any failure.
func ParseAndVerify(samlResponseB64, idpCertificate, spEntityID, acsURL, expectedInResponseTo string) (Identity, error) {
	certPEM := NormalizeCertificatePEM(idpCertificate)
	if certPEM == "" {
		return Identity{}, newErr("No IdP certificate configured")
	}

	xmlBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(samlResponseB64))
	if err != nil {
		// JS Buffer.from(...,'base64') is lenient; retry with raw/url decoders so a
		// padding/charset quirk doesn't masquerade as "malformed" where Node would
		// have decoded it.
		if xmlBytes, err = lenientBase64(samlResponseB64); err != nil {
			return Identity{}, newErr("Malformed SAMLResponse encoding")
		}
	}
	xml := string(xmlBytes)
	if xml == "" || !hasTag.MatchString(xml) {
		return Identity{}, newErr("Empty SAMLResponse")
	}

	doc := etree.NewDocument()
	if err := doc.ReadFromString(xml); err != nil {
		return Identity{}, newErr("Not a SAML Response")
	}
	response := doc.Root()
	if response == nil || response.Tag != "Response" || response.NamespaceURI() != protocolNS {
		return Identity{}, newErr("Not a SAML Response")
	}

	// Status must be Success.
	status := firstChildNS(response, protocolNS, "Status")
	var statusCode *etree.Element
	if status != nil {
		statusCode = firstChildNS(status, protocolNS, "StatusCode")
	}
	if statusCode == nil || statusCode.SelectAttrValue("Value", "") != statusOK {
		return Identity{}, newErr("IdP returned a non-success status")
	}

	assertion := firstChildNS(response, assertionNS, "Assertion")
	if assertion == nil {
		return Identity{}, newErr("No assertion in response")
	}

	if !verifyAssertionSignature(assertion, certPEM) {
		return Identity{}, newErr("Assertion signature verification failed")
	}

	now := time.Now()

	if conditions := firstChildNS(assertion, assertionNS, "Conditions"); conditions != nil {
		if !withinWindow(conditions.SelectAttrValue("NotBefore", ""), conditions.SelectAttrValue("NotOnOrAfter", ""), now) {
			return Identity{}, newErr("Assertion is outside its validity window")
		}
		var audiences []string
		for _, ar := range directChildrenNS(conditions, assertionNS, "AudienceRestriction") {
			for _, aud := range directChildrenNS(ar, assertionNS, "Audience") {
				audiences = append(audiences, textContent(aud))
			}
		}
		if len(audiences) > 0 && spEntityID != "" && !contains(audiences, spEntityID) {
			return Identity{}, newErr("Assertion audience does not match the SP entity ID")
		}
	}

	if scd := firstChildPath(assertion, "Subject", "SubjectConfirmation", "SubjectConfirmationData"); scd != nil {
		recipient := scd.SelectAttrValue("Recipient", "")
		if recipient != "" && acsURL != "" && recipient != acsURL {
			return Identity{}, newErr("Assertion recipient does not match the ACS URL")
		}
		if !withinWindow("", scd.SelectAttrValue("NotOnOrAfter", ""), now) {
			return Identity{}, newErr("Subject confirmation has expired")
		}
		inResponseTo := scd.SelectAttrValue("InResponseTo", "")
		if expectedInResponseTo != "" && inResponseTo != "" && inResponseTo != expectedInResponseTo {
			return Identity{}, newErr("InResponseTo does not match the AuthnRequest")
		}
	}

	nameID := textContent(firstChildPath(assertion, "Subject", "NameID"))
	attributes := map[string]string{}
	if as := firstChildNS(assertion, assertionNS, "AttributeStatement"); as != nil {
		for _, attr := range directChildrenNS(as, assertionNS, "Attribute") {
			name := attr.SelectAttrValue("Name", "")
			value := textContent(firstChildNS(attr, assertionNS, "AttributeValue"))
			if name != "" {
				attributes[strings.ToLower(name)] = value
			}
		}
	}

	email := strings.ToLower(strings.TrimSpace(firstNonEmpty(attributes["email"], nameID)))
	if email == "" {
		return Identity{}, newErr("Assertion carries no email/NameID")
	}
	return Identity{
		Username: strings.TrimSpace(firstNonEmpty(attributes["username"], email)),
		Email:    email,
		Name:     strings.TrimSpace(firstNonEmpty(attributes["name"], email)),
		Role:     strings.ToLower(strings.TrimSpace(attributes["role"])),
	}, nil
}

// ── SP metadata ──────────────────────────────────────────────────────────────

func BuildSpMetadata(spEntityID, acsURL string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="` + escapeXML(spEntityID) + `">` +
		`<md:SPSSODescriptor AuthnRequestsSigned="false" WantAssertionsSigned="true"` +
		` protocolSupportEnumeration="` + protocolNS + `">` +
		`<md:NameIDFormat>` + nameIDEmail + `</md:NameIDFormat>` +
		`<md:AssertionConsumerService Binding="` + bindingPost + `"` +
		` Location="` + escapeXML(acsURL) + `" index="0" isDefault="true"/>` +
		`</md:SPSSODescriptor>` +
		`</md:EntityDescriptor>`
}

// ── helpers ──────────────────────────────────────────────────────────────────

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func lenientBase64(s string) ([]byte, error) {
	t := whitespace.ReplaceAllString(s, "")
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(t); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid base64")
}
