// Generate a RFC 4122 v4 UUID.
//
// `crypto.randomUUID()` and `crypto.subtle` are only exposed in secure contexts
// (HTTPS, or http://localhost). When the app is served over a plain
// `http://<ip>:<port>` LAN address the page is a non-secure context, so
// `crypto.randomUUID` is undefined and calling it throws — which is why actions
// like "Add printer" failed when accessed by IP. `crypto.getRandomValues` is
// available even in non-secure contexts, so fall back to building the UUID from
// it, and only drop to Math.random if crypto is missing entirely.
export function generateId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID();
  }

  const bytes = new Uint8Array(16);
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i += 1) {
      bytes[i] = Math.floor(Math.random() * 256);
    }
  }

  // Set the version (4) and variant (10xx) bits per RFC 4122.
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;

  const hex = Array.from(bytes, (value) => value.toString(16).padStart(2, '0'));
  return (
    `${hex[0]}${hex[1]}${hex[2]}${hex[3]}-${hex[4]}${hex[5]}-${hex[6]}${hex[7]}-` +
    `${hex[8]}${hex[9]}-${hex[10]}${hex[11]}${hex[12]}${hex[13]}${hex[14]}${hex[15]}`
  );
}
