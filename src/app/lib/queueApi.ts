import { QueueData } from '../types';
import { logAuditEvent } from './auditApi';

async function readJsonResponse<T>(response: Response): Promise<T> {
  if (!response.ok) {
    let message = `Request failed with ${response.status}`;

    try {
      const payload = (await response.json()) as { error?: string };
      if (payload.error) {
        message = payload.error;
      }
    } catch {
      // Ignore invalid JSON error bodies.
    }

    throw new Error(message);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}

export async function fetchQueueJobs(): Promise<QueueData> {
  const response = await fetch('/api/queue', {
    cache: 'no-store',
    headers: {
      'Cache-Control': 'no-cache',
      Pragma: 'no-cache',
    },
  });

  return readJsonResponse<QueueData>(response);
}

// Trigger a Google Sheet pull server-side, then return the refreshed queue.
// Use this for on-demand refresh (e.g. opening the Queue page); routine polling
// should use the cheap read-only fetchQueueJobs() above, since a background loop
// keeps the stored queue in sync with the Sheet.
export async function syncQueueJobs(): Promise<QueueData> {
  const response = await fetch('/api/queue/sync', {
    method: 'POST',
    cache: 'no-store',
  });

  return readJsonResponse<QueueData>(response);
}

export async function markQueueJobAsPrinted(jobId: string) {
  const response = await fetch(`/api/queue/${encodeURIComponent(jobId)}/printed`, {
    method: 'POST',
  });

  await readJsonResponse<void>(response);
  logAuditEvent('queue.mark_printed', jobId);
}

export async function resetQueueJobStatuses() {
  const response = await fetch('/api/queue/reset', {
    method: 'POST',
  });

  await readJsonResponse<void>(response);
  logAuditEvent('queue.reset');
}

export async function deleteQueueJob(jobId: string) {
  const response = await fetch(`/api/queue/${encodeURIComponent(jobId)}`, {
    method: 'DELETE',
  });

  await readJsonResponse<void>(response);
  logAuditEvent('queue.delete', jobId);
}
