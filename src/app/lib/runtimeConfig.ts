export const PUBLIC_VIEWER_MODE = import.meta.env.VITE_PUBLIC_VIEWER_MODE === 'true';

// Default admin password hash (sha256 hex). Set VITE_ADMIN_PASSWORD_HASH at build
// time to your own hash so a publicly known default isn't shipped in the bundle.
// Generate one with:
//   node -e "console.log(require('node:crypto').createHash('sha256').update(process.argv[1]).digest('hex'))" "your-password"
// Falls back to the default first-run credentials (admin / "admin") when unset so
// a fresh build can log in; set your own hash for any real deployment.
export const ADMIN_PASSWORD_HASH =
  (import.meta.env.VITE_ADMIN_PASSWORD_HASH as string | undefined) ||
  '8c6976e5b5410415bde908bd4dee15dfb167a9c873fc4bb8a81f6f2ab448a918';

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
