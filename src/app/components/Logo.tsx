import { useBrandingSettings } from '../lib/settingsApi';
import defaultLogo from '../assets/printer-logo.svg';

interface LogoProps {
  // Rendered height in px at scale 1; the branding scale is applied on top.
  baseHeight: number;
  alt?: string;
  className?: string;
}

// Force the logo to a single theme color: solid dark in light mode, solid white
// in dark mode. `brightness-0` flattens any logo (raster, single- or multi-color
// SVG) to black; `dark:invert` flips that to white when the dark theme is active.
const THEME_LOGO_FILTER = 'brightness-0 dark:invert';

// Renders the active site logo (uploaded SVG inlined, uploaded raster as <img>,
// or the bundled default), always recolored to follow the theme. The branding
// `logoScale` multiplies `baseHeight`.
export function Logo({ baseHeight, alt = 'PrintFarm logo', className = '' }: LogoProps) {
  const { logoDataUrl, logoSvg, logoScale } = useBrandingSettings();
  const height = Math.round(baseHeight * (logoScale || 1));

  if (logoSvg) {
    return (
      <span
        role="img"
        aria-label={alt}
        style={{ height }}
        className={`inline-flex items-center [&>svg]:h-full [&>svg]:w-auto [&>svg]:max-w-full ${THEME_LOGO_FILTER} ${className}`}
        dangerouslySetInnerHTML={{ __html: logoSvg }}
      />
    );
  }

  const src = logoDataUrl || defaultLogo;
  return (
    <img
      src={src}
      alt={alt}
      style={{ height }}
      className={`w-auto max-w-full ${THEME_LOGO_FILTER} ${className}`}
    />
  );
}
