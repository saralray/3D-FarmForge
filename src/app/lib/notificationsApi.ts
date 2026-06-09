import { logAuditEvent } from './auditApi';

export interface DiscordWebhook {
  id: string;
  name: string;
  webhookUrl: string;
  // null/undefined => every event is sent (historical default). An array
  // restricts the webhook to the listed event keys.
  events?: string[] | null;
  createdAt?: string;
}

// Canonical catalog of notification events a webhook can subscribe to. Keys must
// match those emitted by the poller (printer status/job transitions, filament,
// temperature) and the web server (queue submissions).
export interface NotificationEvent {
  key: string;
  label: string;
}

export const NOTIFICATION_EVENTS: NotificationEvent[] = [
  { key: 'print_started', label: 'Print started' },
  { key: 'print_completed', label: 'Print completed / stopped' },
  { key: 'print_cancelled', label: 'Print cancelled' },
  { key: 'print_paused', label: 'Print paused' },
  { key: 'print_resumed', label: 'Print resumed' },
  { key: 'filament_runout', label: 'Out of filament' },
  { key: 'temp_target_reached', label: 'Temperature reached target' },
  { key: 'printer_online', label: 'Printer online' },
  { key: 'printer_offline', label: 'Printer offline' },
  { key: 'queue_added', label: 'New queue submission' },
];

export const NOTIFICATION_EVENT_KEYS = NOTIFICATION_EVENTS.map((event) => event.key);

async function parseError(response: Response) {
  try {
    const payload = await response.json() as { error?: string }
    return payload.error ?? `Request failed with ${response.status}`
  } catch {
    return `Request failed with ${response.status}`
  }
}

export async function fetchDiscordWebhooks(): Promise<DiscordWebhook[]> {
  const response = await fetch('/api/notifications/discord-webhooks', {
    cache: 'no-store',
  })

  if (!response.ok) {
    throw new Error(await parseError(response))
  }

  return response.json() as Promise<DiscordWebhook[]>
}

export async function saveDiscordWebhook(webhook: DiscordWebhook) {
  const response = await fetch('/api/notifications/discord-webhooks', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(webhook),
  })

  if (!response.ok) {
    throw new Error(await parseError(response))
  }

  logAuditEvent('webhook.save', webhook.name, { id: webhook.id })
}

export async function removeDiscordWebhook(webhookId: string) {
  const response = await fetch(`/api/notifications/discord-webhooks/${encodeURIComponent(webhookId)}`, {
    method: 'DELETE',
  })

  if (!response.ok) {
    throw new Error(await parseError(response))
  }

  logAuditEvent('webhook.delete', webhookId)
}
