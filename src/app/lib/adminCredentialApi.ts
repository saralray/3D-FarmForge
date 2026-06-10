// Admin bootstrap credential, stored server-side (DB) and set through the website
// on first run — never baked into the frontend bundle. The client only ever sends
// a sha256 hash of the password; the plaintext stays in the browser.

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

// Whether the admin password has been set yet. Drives the first-run setup screen.
export async function fetchAdminConfigured(): Promise<boolean> {
  try {
    const response = await fetch('/api/admin/credential', { cache: 'no-store' });
    if (!response.ok) {
      return false;
    }
    const data = (await response.json()) as { configured?: boolean };
    return Boolean(data.configured);
  } catch {
    return false;
  }
}

// First-run setup. Succeeds only while no admin password exists; the server
// returns 409 once one is configured.
export async function setupAdminCredential(passwordHash: string): Promise<MutationResult> {
  const response = await fetch('/api/admin/credential', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ passwordHash }),
  });
  return response.ok ? { ok: true } : { ok: false, error: await readError(response) };
}

// Login check. Returns true only when the hash matches the stored credential.
export async function verifyAdminCredential(passwordHash: string): Promise<boolean> {
  try {
    const response = await fetch('/api/admin/credential/verify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ passwordHash }),
    });
    return response.ok;
  } catch {
    return false;
  }
}

// Change the admin password. The server requires the current password hash to
// authorize the change.
export async function changeAdminCredential(
  currentPasswordHash: string,
  newPasswordHash: string,
): Promise<MutationResult> {
  const response = await fetch('/api/admin/credential', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ currentPasswordHash, newPasswordHash }),
  });
  return response.ok ? { ok: true } : { ok: false, error: await readError(response) };
}
