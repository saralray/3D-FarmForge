// Slicer operator-grant tokens.
//
// When a slicer opens its "Device" tab, the slicer-proxy redirects the embedded
// browser to the dashboard so the lab user lands as an operator. That hand-off
// must not be a constant, forgeable URL flag (anyone could append it to any
// dashboard URL and self-promote). Instead the proxy mints a short-lived,
// HMAC-signed token bound to the printer id, and the web server verifies it
// before establishing the operator session.
//
// Both the proxy (minter) and the web server (verifier) import this module and
// share SLICER_GRANT_SECRET. With no secret set the feature fails closed: minting
// throws and verification rejects, so the slicer user simply falls back to a
// viewer session instead of getting an unauthenticated operator grant.

import { createHmac, timingSafeEqual } from 'node:crypto';

const SECRET = (process.env.SLICER_GRANT_SECRET || '').trim();

// Tokens are single-hop and consumed immediately on landing, so a tight window
// is enough to cover the redirect while leaving no bookmarkable grant behind.
const TOKEN_TTL_MS = 2 * 60 * 1000;

function sign(payload) {
  return createHmac('sha256', SECRET).update(payload).digest('base64url');
}

export function isSlicerGrantConfigured() {
  return SECRET.length > 0;
}

// Mint a grant for one printer. Throws when no secret is configured so callers
// can fall back to a plain (operator-less) redirect rather than emit a token
// nobody can verify.
export function mintSlicerGrant(printerId) {
  if (!SECRET) {
    throw new Error('SLICER_GRANT_SECRET is not configured');
  }
  const payload = Buffer.from(
    JSON.stringify({ pid: String(printerId), exp: Date.now() + TOKEN_TTL_MS }),
  ).toString('base64url');
  return `${payload}.${sign(payload)}`;
}

// Verify a token. Returns { printerId } when the signature is valid and the
// token has not expired; returns null for anything else (no secret, malformed,
// bad signature, expired).
export function verifySlicerGrant(token) {
  if (!SECRET || typeof token !== 'string') {
    return null;
  }
  const separator = token.indexOf('.');
  if (separator === -1) {
    return null;
  }

  const payload = token.slice(0, separator);
  const signature = token.slice(separator + 1);
  const expected = sign(payload);

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

  if (!data || typeof data.pid !== 'string' || typeof data.exp !== 'number') {
    return null;
  }
  if (Date.now() > data.exp) {
    return null;
  }

  return { printerId: data.pid };
}
