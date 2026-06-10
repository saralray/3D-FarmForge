export const PUBLIC_VIEWER_MODE = import.meta.env.VITE_PUBLIC_VIEWER_MODE === 'true';

// The admin username for the single bootstrap account. Its password is no longer
// baked into the bundle — it is set through the website on first run and stored
// server-side (see lib/adminCredentialApi.ts).
export const ADMIN_USERNAME = 'admin';

export const PUBLIC_VIEWER_USER = {
  id: 'public-viewer',
  name: 'Viewer',
  username: 'viewer',
  role: 'viewer' as const,
};

// Identity granted when the dashboard is opened from a slicer's "Device" tab.
// The slicer-proxy redirects that webview here with `?slicer_grant=<token>`,
// where the token is a short-lived, HMAC-signed grant the web server verifies
// before the dashboard promotes the lab user to operator (pause/resume/cancel).
// A constant flag is deliberately not used — it would let anyone self-promote
// by appending it to a dashboard URL.
export const SLICER_OPERATOR_GRANT_PARAM = 'slicer_grant';

export const SLICER_OPERATOR_USER = {
  id: 'slicer-operator',
  name: 'Slicer Operator',
  username: 'slicer',
  role: 'operator' as const,
};
