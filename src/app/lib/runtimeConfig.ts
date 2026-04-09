export const PUBLIC_VIEWER_MODE = import.meta.env.VITE_PUBLIC_VIEWER_MODE === 'true';

export const PUBLIC_VIEWER_USER = {
  id: 'public-viewer',
  name: 'Public Viewer',
  username: 'public',
  role: 'viewer' as const,
};
