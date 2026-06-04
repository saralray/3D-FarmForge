export const PUBLIC_VIEWER_MODE = import.meta.env.VITE_PUBLIC_VIEWER_MODE === 'true';

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
