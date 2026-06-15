import { useBrandingSettings } from '../lib/settingsApi';
import defaultLogo from '../assets/printer-logo.svg';

interface LogoProps {
  // Rendered height in px at scale 1; the branding scale is applied on top.
  baseHeight: number;
  alt?: string;
  className?: string;
}

// Renders the active site logo, in priority order:
//   1. An uploaded SVG, inlined so monochrome marks adapt to the theme via
//      currentColor (when the server flagged it adaptive).
//   2. An uploaded raster image (PNG/JPEG/…), shown as-is.
//   3. The bundled default printer icon, inverted in dark mode.
// The branding `logoScale` multiplies `baseHeight` in every case.
export function Logo({ baseHeight, alt = 'PrintFarm logo', className = '' }: LogoProps) {
  const { logoDataUrl, logoSvg, logoAdaptive, logoScale } = useBrandingSettings();
  const height = Math.round(baseHeight * (logoScale || 1));

  if (logoSvg) {
    return (
      <span
        role="img"
        aria-label={alt}
        style={{ height }}
        className={`inline-flex items-center [&>svg]:h-full [&>svg]:w-auto [&>svg]:max-w-full ${
          logoAdaptive ? 'text-gray-900 dark:text-white' : ''
        } ${className}`}
        dangerouslySetInnerHTML={{ __html: logoSvg }}
      />
    );
  }

  if (logoDataUrl) {
    return (
      <img src={logoDataUrl} alt={alt} style={{ height }} className={`w-auto max-w-full ${className}`} />
    );
  }

  return (
    <img
      src={defaultLogo}
      alt={alt}
      style={{ height }}
      className={`w-auto max-w-full dark:invert dark:brightness-200 ${className}`}
    />
  );
}
