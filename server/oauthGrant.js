// OAuth callback tokens (SSO sign-in — Google or Microsoft Entra ID).
//
// The dashboard's auth is cookieless: there is no server-side session store, so
// the OAuth Authorization Code flow is bridged into the client the same way the
// slicer hand-off is (see slicerGrant.js). Two short-lived, HMAC-signed tokens
// carry the flow (the provider — `google` or `microsoft` — rides along in both):
//
//   - a `state` token, minted in /api/auth/:provider/start and echoed back by the
//     identity provider to /api/auth/:provider/callback, that proves the callback
//     corresponds to a request we initiated (CSRF protection);
//   - an `auth grant` token, minted in the callback once the provider has
//     authenticated the user, handed to the browser as `?oauth_grant=<token>` and
//     verified by /api/auth/verify before the client establishes a session.
//
// Unlike slicerGrant.js (which reads its secret from the environment), these
// helpers take the signing secret as an argument — the web server reads it from
// app_settings (generating one on first use), so OAuth needs no extra env var.

import { createHmac, timingSafeEqual } from 'node:crypto';

// State covers the round-trip to the provider's consent screen, which a user may
// sit on for a while; the grant is single-hop and consumed immediately on landing.
const STATE_TTL_MS = 10 * 60 * 1000;
const GRANT_TTL_MS = 2 * 60 * 1000;

function sign(secret, payload) {
  return createHmac('sha256', secret).update(payload).digest('base64url');
}

function encode(secret, data) {
  const payload = Buffer.from(JSON.stringify(data)).toString('base64url');
  return `${payload}.${sign(secret, payload)}`;
}

// Verify signature + expiry and return the decoded payload, or null for anything
// invalid (no secret, malformed, bad signature, expired, missing `exp`).
function decode(secret, token) {
  if (!secret || typeof token !== 'string') {
    return null;
  }
  const separator = token.indexOf('.');
  if (separator === -1) {
    return null;
  }

  const payload = token.slice(0, separator);
  const signature = token.slice(separator + 1);
  const expected = sign(secret, payload);

  const signatureBuffer = Buffer.from(signature);
  const expectedBuffer = Buffer.from(expected);
  if (
    signatureBuffer.length !== expectedBuffer.length ||
    !timingSafeEqual(signatureBuffer, expectedBuffer)
  ) {
    return null;
  }

  let data;
  try {
    data = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'));
  } catch {
    return null;
  }

  if (!data || typeof data.exp !== 'number' || Date.now() > data.exp) {
    return null;
  }
  return data;
}

// State token: binds a short nonce (and, optionally, where to return afterwards)
// to the request we initiated. The callback verifies the signature; the nonce is
// carried so a tampered/forged state can't be replayed past expiry.
export function signState(secret, extra = {}) {
  return encode(secret, { ...extra, kind: 'state', exp: Date.now() + STATE_TTL_MS });
}

export function verifyState(secret, token) {
  const data = decode(secret, token);
  return data && data.kind === 'state' ? data : null;
}

// Auth grant: the authenticated identity handed to the client after the provider
// verifies the user. Carries the fields the frontend needs to build its session,
// including which `provider` issued it (so the verify endpoint can namespace the
// user id, e.g. `google:<sub>` vs `microsoft:<sub>`).
export function mintAuthGrant(secret, { provider, sub, email, name, role }) {
  return encode(secret, {
    kind: 'grant',
    provider,
    sub,
    email,
    name,
    role,
    exp: Date.now() + GRANT_TTL_MS,
  });
}

export function verifyAuthGrant(secret, token) {
  const data = decode(secret, token);
  if (!data || data.kind !== 'grant' || typeof data.email !== 'string' || !data.email) {
    return null;
  }
  return {
    provider: typeof data.provider === 'string' ? data.provider : 'google',
    sub: typeof data.sub === 'string' ? data.sub : data.email,
    email: data.email,
    name: typeof data.name === 'string' ? data.name : data.email,
    role: typeof data.role === 'string' ? data.role : 'student',
  };
}
