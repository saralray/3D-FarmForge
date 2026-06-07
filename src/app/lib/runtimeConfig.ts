export const PUBLIC_VIEWER_MODE = import.meta.env.VITE_PUBLIC_VIEWER_MODE === 'true';

// Default admin password hash (sha256 hex). Set VITE_ADMIN_PASSWORD_HASH at build
// time to your own hash so a publicly known default isn't shipped in the bundle.
// Generate one with:
//   node -e "console.log(require('node:crypto').createHash('sha256').update(process.argv[1]).digest('hex'))" "your-password"
// Falls back to the legacy default ("stemlab") when unset so existing deploys
// keep working until they configure their own.
export const ADMIN_PASSWORD_HASH =
  (import.meta.env.VITE_ADMIN_PASSWORD_HASH as string | undefined) ||
  '247be42a8460b48531c8e35c3e494a0c86dd70b65b4f234ed4bc73474b76d994';

export const PUBLIC_VIEWER_USER = {
  id: 'public-viewer',
  name: 'Viewer',
  username: 'viewer',
  role: 'viewer' as const,
};

// Identity granted when the dashboard is opened from a slicer's "Device" tab.
// The slicer-proxy redirects that webview here with `?slicer_access=operator`,
// landing the lab user as an operator (pause/resume/cancel) on the printer's
// management page rather than a read-only viewer.
export const SLICER_OPERATOR_GRANT_PARAM = 'slicer_access';
export const SLICER_OPERATOR_GRANT_VALUE = 'operator';

export const SLICER_OPERATOR_USER = {
  id: 'slicer-operator',
  name: 'Slicer Operator',
  username: 'slicer',
  role: 'operator' as const,
};
