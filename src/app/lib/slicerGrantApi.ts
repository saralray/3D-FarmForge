// Verifies a slicer operator-grant token with the server. The token arrives as a
// `?slicer_grant=<token>` query param when a slicer's "Device" tab redirects into
// the dashboard; the server checks its HMAC signature and expiry. Returns true
// only when the grant is valid, so the client never promotes to operator on an
// unverified (or forged) token.
export async function verifySlicerGrant(token: string): Promise<boolean> {
  try {
    const response = await fetch('/api/slicer-grant/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    });
    return response.ok;
  } catch {
    return false;
  }
}
