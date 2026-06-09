// Client-side audit logging. Identity lives only in the browser (auth is
// client-side), so each action reports the current actor along with a short
// machine-readable `action`, an optional human `target`, and structured
// `details`. Entries are appended via POST /api/audit-logs and read back on the
// admin-only Logs page. Page changes are deliberately NOT logged — only actions.

export interface AuditActor {
  name?: string | null;
  username?: string | null;
  role?: string | null;
}

export interface AuditLogEntry {
  id: number;
  createdAt: string;
  actorName: string | null;
  actorUsername: string | null;
  actorRole: string | null;
  action: string;
  target: string | null;
  details: Record<string, unknown> | null;
  source: string;
  ip: string | null;
}

// The current actor is set by AuthContext whenever the signed-in user changes,
// so action sites can call logAuditEvent without threading the user through.
let currentActor: AuditActor = {};

export function setAuditActor(actor: AuditActor | null) {
  currentActor = actor ?? {};
}

// Fire-and-forget: an audit write must never break or delay the user's action,
// so failures are swallowed.
export function logAuditEvent(
  action: string,
  target?: string | null,
  details?: Record<string, unknown> | null,
): void {
  void fetch('/api/audit-logs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      actor: {
        name: currentActor.name ?? null,
        username: currentActor.username ?? null,
        role: currentActor.role ?? null,
      },
      action,
      target: target ?? null,
      details: details ?? null,
    }),
  }).catch(() => {
    // Ignore — logging is best-effort.
  });
}

export async function fetchAuditLogs(limit = 200): Promise<AuditLogEntry[]> {
  const response = await fetch(`/api/audit-logs?limit=${encodeURIComponent(limit)}`, {
    cache: 'no-store',
    headers: {
      'Cache-Control': 'no-cache',
      Pragma: 'no-cache',
    },
  });

  if (!response.ok) {
    throw new Error(`Request failed with ${response.status}`);
  }

  return response.json() as Promise<AuditLogEntry[]>;
}
