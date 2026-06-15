import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './app/App';
import './styles/index.css';

// Detect when the app is launched as an installed/standalone web app
// (Add to Home Screen on iOS, or an installed PWA) so the UI can switch to a
// more compact, app-like layout. iOS exposes navigator.standalone; other
// platforms report it via the standalone display-mode media query.
function applyStandaloneClass() {
  const isStandalone =
    window.matchMedia('(display-mode: standalone)').matches ||
    (window.navigator as Navigator & { standalone?: boolean }).standalone === true;
  document.documentElement.classList.toggle('pwa-standalone', isStandalone);
}

applyStandaloneClass();
window.matchMedia('(display-mode: standalone)').addEventListener('change', applyStandaloneClass);

const rootElement = document.getElementById('root');

if (!rootElement) {
  throw new Error('Root element #root was not found.');
}

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>
);
