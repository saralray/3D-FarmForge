import { useEffect } from 'react';
import { DEFAULT_SITE_NAME, useBrandingSettings } from '../lib/settingsApi';

// Applies the runtime branding (site name) to the document. It renders nothing —
// it just keeps `document.title` in sync with the stored branding, so the name
// follows the admin's choice everywhere (login, dashboard, browser tab) without
// per-page wiring.
export function BrandingApplier() {
  const { siteName } = useBrandingSettings();

  useEffect(() => {
    document.title = siteName || DEFAULT_SITE_NAME;
  }, [siteName]);

  return null;
}
