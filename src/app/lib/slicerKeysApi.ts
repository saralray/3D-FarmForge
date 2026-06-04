export interface SlicerApiKey {
  id: string;
  name: string;
  keyPrefix: string;
  lastUsedAt?: string | null;
  createdAt?: string;
}

// Returned only by createSlicerKey — the full key is shown once and never again.
export interface CreatedSlicerKey {
  id: string;
  name: string;
  key: string;
}

async function parseError(response: Response) {
  try {
    const payload = (await response.json()) as { error?: string };
    return payload.error ?? `Request failed with ${response.status}`;
  } catch {
    return `Request failed with ${response.status}`;
  }
}

export async function fetchSlicerKeys(): Promise<SlicerApiKey[]> {
  const response = await fetch('/api/slicer-keys', { cache: 'no-store' });

  if (!response.ok) {
    throw new Error(await parseError(response));
  }

  return response.json() as Promise<SlicerApiKey[]>;
}

export async function createSlicerKey(name: string): Promise<CreatedSlicerKey> {
  const response = await fetch('/api/slicer-keys', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ name }),
  });

  if (!response.ok) {
    throw new Error(await parseError(response));
  }

  return response.json() as Promise<CreatedSlicerKey>;
}

export async function removeSlicerKey(keyId: string) {
  const response = await fetch(`/api/slicer-keys/${encodeURIComponent(keyId)}`, {
    method: 'DELETE',
  });

  if (!response.ok) {
    throw new Error(await parseError(response));
  }
}
