import { useEffect, useState } from 'react';
import { GOOGLE_FORM_URL } from './runtimeConfig';

export interface IntegrationSettings {
  googleSheetQueueUrl: string;
  googleFormUrl: string;
}

// Falls back to the build-time env defaults so consumers always have a usable
// value even before the API responds (or if the server is briefly unavailable).
const DEFAULT_INTEGRATION_SETTINGS: IntegrationSettings = {
  googleSheetQueueUrl: import.meta.env.VITE_GOOGLE_SHEET_QUEUE_URL || '',
  googleFormUrl: GOOGLE_FORM_URL,
};

async function parseError(response: Response) {
  try {
    const payload = (await response.json()) as { error?: string };
    return payload.error ?? `Request failed with ${response.status}`;
  } catch {
    return `Request failed with ${response.status}`;
  }
}

export async function fetchIntegrationSettings(): Promise<IntegrationSettings> {
  const response = await fetch('/api/settings/integrations', { cache: 'no-store' });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  return response.json() as Promise<IntegrationSettings>;
}

export async function saveIntegrationSettings(
  settings: IntegrationSettings,
): Promise<IntegrationSettings> {
  const response = await fetch('/api/settings/integrations', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  return response.json() as Promise<IntegrationSettings>;
}

// Read-only hook for the components that just need the effective URLs (Login,
// Navigation, Queue). Starts from the env defaults and refreshes from the API.
export function useIntegrationSettings(): IntegrationSettings {
  const [settings, setSettings] = useState<IntegrationSettings>(DEFAULT_INTEGRATION_SETTINGS);

  useEffect(() => {
    let active = true;
    fetchIntegrationSettings()
      .then((value) => {
        if (active) {
          setSettings(value);
        }
      })
      .catch(() => {
        // Keep the env-derived defaults on failure.
      });
    return () => {
      active = false;
    };
  }, []);

  return settings;
}
