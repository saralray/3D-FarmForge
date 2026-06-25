import { useEffect } from 'react';
import { DEFAULT_SITE_NAME, useBrandingSettings } from '../lib/settingsApi';

// Applies the runtime branding (site name + favicon) to the document. It renders
// nothing — it just keeps `document.title` and the favicon `<link>` in sync with
// the stored branding so the name and icon follow the admin's choice everywhere.
export function BrandingApplier() {
  const { siteName, faviconDataUrl } = useBrandingSettings();

  useEffect(() => {
    document.title = siteName || DEFAULT_SITE_NAME;
  }, [siteName]);

  useEffect(() => {
    const links = document.querySelectorAll<HTMLLinkElement>('link[rel~="icon"]');
    if (faviconDataUrl) {
      if (links.length > 0) {
        links.forEach((link) => {
          link.href = faviconDataUrl;
          link.type = '';
        });
      } else {
        const link = document.createElement('link');
        link.rel = 'icon';
        link.href = faviconDataUrl;
        document.head.appendChild(link);
      }
    } else {
      links.forEach((link) => {
        link.href = '/icon.svg';
        link.type = 'image/svg+xml';
      });
    }
  }, [faviconDataUrl]);

  return null;
}
