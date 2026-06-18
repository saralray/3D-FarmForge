// Google OAuth sign-in. The dashboard auth is cookieless, so the server runs the
// Authorization Code flow and hands the authenticated identity back to the client
// as a short-lived, HMAC-signed grant token in a URL param (`?oauth_grant=`). The
// client verifies it here (server-side) before creating a session — the same
// hand-off shape as the slicer grant. See server/oauthGrant.js.

import type { UserRole } from './usersApi';

export interface OAuthUser {
  id: string;
  name: string;
  username: string;
  role: UserRole;
}

// Admin-facing config shape for the Settings → Integrations form. The client
// secret is never returned — only whether one is stored.
export interface OAuthSettings {
  enabled: boolean;
  clientId: string;
  allowedDomains: string[];
  hasClientSecret: boolean;
}

export interface OAuthSettingsInput {
  enabled: boolean;
  clientId: string;
  // Blank means "keep the stored secret"; a value replaces it.
  clientSecret: string;
  allowedDomains: string[];
}

interface MutationResult {
  ok: boolean;
  error?: string;
}

async function readError(response: Response): Promise<string | undefined> {
  try {
    const payload = (await response.json()) as { error?: string };
    return payload.error;
  } catch {
    return undefined;
  }
}

// Whether Google sign-in is configured + enabled. Drives the "Sign in with
// Google" button on the login page.
export async function fetchOAuthEnabled(): Promise<boolean> {
  try {
    const response = await fetch('/api/auth/google/config', { cache: 'no-store' });
    if (!response.ok) {
      return false;
    }
    const data = (await response.json()) as { enabled?: boolean };
    return Boolean(data.enabled);
  } catch {
    return false;
  }
}

// Verify the grant token carried back from the OAuth callback. Returns the user
// to sign in as, or null when the token is missing/forged/expired.
export async function verifyGoogleGrant(token: string): Promise<OAuthUser | null> {
  try {
    const response = await fetch('/api/auth/google/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    });
    if (!response.ok) {
      return null;
    }
    const data = (await response.json()) as { user?: OAuthUser };
    return data.user ?? null;
  } catch {
    return null;
  }
}

// Admin config read (Settings → Integrations).
export async function fetchOAuthSettings(): Promise<OAuthSettings> {
  const response = await fetch('/api/settings/oauth', { cache: 'no-store' });
  if (!response.ok) {
    throw new Error((await readError(response)) ?? 'Unable to load OAuth settings.');
  }
  return response.json() as Promise<OAuthSettings>;
}

export async function saveOAuthSettings(input: OAuthSettingsInput): Promise<OAuthSettings> {
  const response = await fetch('/api/settings/oauth', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error((await readError(response)) ?? 'Unable to save OAuth settings.');
  }
  return response.json() as Promise<OAuthSettings>;
}

export type { MutationResult };
