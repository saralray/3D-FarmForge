export interface DiscordWebhook {
  id: string;
  name: string;
  webhookUrl: string;
  createdAt?: string;
}

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
}

export async function removeDiscordWebhook(webhookId: string) {
  const response = await fetch(`/api/notifications/discord-webhooks/${encodeURIComponent(webhookId)}`, {
    method: 'DELETE',
  })

  if (!response.ok) {
    throw new Error(await parseError(response))
  }
}
