// Minimal service worker — required for Chrome Android "Add to Home Screen" prompt.
// Network-first: always fetch from network, cache only on success.
const CACHE = 'farmlab-v1';

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', e => e.waitUntil(self.clients.claim()));

self.addEventListener('fetch', e => {
  // Only cache same-origin GET requests for static assets.
  const { request } = e;
  if (request.method !== 'GET') return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  // Skip API, webcam, proxy, and metrics routes.
  if (/^\/(api|__printer|metrics|healthz|readyz|prometheus)/.test(url.pathname)) return;

  e.respondWith(
    fetch(request)
      .then(res => {
        if (res.ok) {
          const clone = res.clone();
          caches.open(CACHE).then(c => c.put(request, clone));
        }
        return res;
      })
      .catch(() => caches.match(request))
  );
});
