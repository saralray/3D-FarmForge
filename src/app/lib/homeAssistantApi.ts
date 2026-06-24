import { logAuditEvent } from './auditApi';

export interface HomeAssistantConfig {
  baseUrl: string;
  enabled: boolean;
  // The token is never returned by the server; this only reports whether one is
  // stored so the form can show "configured" without echoing the secret.
  hasToken: boolean;
}

// What the form sends on save. A blank `token` means "keep the stored one".
export interface HomeAssistantConfigInput {
  baseUrl: string;
  token: string;
  enabled: boolean;
}

export interface HaEntity {
  entityId: string;
  domain: string;
  friendlyName: string;
  state: string;
}

export interface HaDevices {
  entities: HaEntity[];
  groups: Record<string, HaEntity[]>;
}

export type HaRuleDirection = 'ha_to_printer' | 'printer_to_ha';

// A print-farm ⇄ Home Assistant automation rule. The fields used depend on
// `direction`; the server validates per direction.
export interface HaRule {
  id: string;
  name: string;
  direction: HaRuleDirection;
  enabled: boolean;
  printerId: string;
  createdAt?: string;
  // ha_to_printer
  triggerEntity?: string;
  triggerState?: string;
  printerCommand?: 'pause' | 'resume' | 'cancel';
  // printer_to_ha
  printerStatus?: 'printing' | 'idle' | 'paused' | 'error' | 'offline';
  actionService?: string;
  actionEntity?: string;
  actionData?: unknown;
}

// What the builder submits. `actionData` is a JSON string the server parses.
export interface HaRuleInput {
  name: string;
  direction: HaRuleDirection;
  enabled: boolean;
  printerId: string;
  triggerEntity?: string;
  triggerState?: string;
  printerCommand?: 'pause' | 'resume' | 'cancel';
  printerStatus?: 'printing' | 'idle' | 'paused' | 'error' | 'offline';
  actionService?: string;
  actionEntity?: string;
  actionData?: string;
}

const BASE = '/api/settings/home-assistant';

async function parseError(response: Response) {
  try {
    const payload = (await response.json()) as { error?: string };
    return payload.error ?? `Request failed with ${response.status}`;
  } catch {
    return `Request failed with ${response.status}`;
  }
}

export async function fetchHomeAssistantConfig(): Promise<HomeAssistantConfig> {
  const response = await fetch(BASE, { cache: 'no-store' });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  return response.json() as Promise<HomeAssistantConfig>;
}

export async function saveHomeAssistantConfig(
  input: HomeAssistantConfigInput,
): Promise<HomeAssistantConfig> {
  const response = await fetch(BASE, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  logAuditEvent('settings.home-assistant');
  return response.json() as Promise<HomeAssistantConfig>;
}

export async function testHomeAssistantConnection(): Promise<{ ok: boolean; message?: string; error?: string }> {
  const response = await fetch(`${BASE}/test`, { method: 'POST' });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  return response.json() as Promise<{ ok: boolean; message?: string; error?: string }>;
}

export async function fetchHomeAssistantDevices(): Promise<HaDevices> {
  const response = await fetch(`${BASE}/devices`, { cache: 'no-store' });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  return response.json() as Promise<HaDevices>;
}

export async function fetchHaRules(): Promise<HaRule[]> {
  const response = await fetch(`${BASE}/rules`, { cache: 'no-store' });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  const payload = (await response.json()) as { rules?: HaRule[] };
  return payload.rules ?? [];
}

export async function createHaRule(input: HaRuleInput): Promise<HaRule> {
  const response = await fetch(`${BASE}/rules`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  logAuditEvent('settings.home-assistant.rule.create');
  return response.json() as Promise<HaRule>;
}

export async function setHaRuleEnabled(id: string, enabled: boolean): Promise<HaRule> {
  const response = await fetch(`${BASE}/rules/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  logAuditEvent('settings.home-assistant.rule.update');
  return response.json() as Promise<HaRule>;
}

export async function deleteHaRule(id: string): Promise<void> {
  const response = await fetch(`${BASE}/rules/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
  if (!response.ok) {
    throw new Error(await parseError(response));
  }
  logAuditEvent('settings.home-assistant.rule.delete');
}
