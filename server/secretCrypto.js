// Application-layer envelope encryption for printer connection secrets at rest —
// specifically the LAN access code / API key stored in printers.api_key_header,
// which is the password used to reach a printer's camera, MQTT and FTP. A leaked
// database dump should not hand an attacker every printer's credentials in clear.
//
// The key is shared across the web, slicer-proxy and poller services through the
// PRINTER_SECRET_KEY env var; all three must agree on it or a Bambu printer's
// camera/MQTT/FTP auth breaks. When the var is unset, encryption is DISABLED and
// values are stored/read as plaintext, so an existing deployment keeps working
// unchanged until a key is provisioned.
//
// Stored format (AES-256-GCM, all base64):  enc:v1:<iv>:<ciphertext>:<tag>
// The Python poller (poller/printer_status_poller.py) implements the identical
// format, so a value written by either side decrypts on the other. decryptSecret
// passes any value not in this format straight through, so pre-encryption
// plaintext rows stay readable and get encrypted on their next write.

import { createCipheriv, createDecipheriv, createHash, randomBytes } from 'node:crypto';

const ENC_PREFIX = 'enc:v1:';

// Resolve the 32-byte AES key from PRINTER_SECRET_KEY. Accepts a 64-char hex
// string or a 32-byte base64 value directly; any other non-empty value is hashed
// with sha256 so an arbitrary passphrase still yields a valid key length.
function loadKey() {
  const raw = (process.env.PRINTER_SECRET_KEY || '').trim();
  if (!raw) {
    return null;
  }
  if (/^[0-9a-fA-F]{64}$/.test(raw)) {
    return Buffer.from(raw, 'hex');
  }
  const b64 = Buffer.from(raw, 'base64');
  if (b64.length === 32) {
    return b64;
  }
  return createHash('sha256').update(raw).digest();
}

const KEY = loadKey();

export function isEncryptionEnabled() {
  return KEY !== null;
}

export function isEncrypted(value) {
  return typeof value === 'string' && value.startsWith(ENC_PREFIX);
}

// Encrypt a plaintext secret for storage. Returns the value unchanged when
// encryption is disabled, the input is empty/not a string, or it is already
// encrypted — so callers can apply it unconditionally on the write path.
export function encryptSecret(plain) {
  if (typeof plain !== 'string' || plain === '' || !KEY || isEncrypted(plain)) {
    return plain;
  }
  const iv = randomBytes(12);
  const cipher = createCipheriv('aes-256-gcm', KEY, iv);
  const ciphertext = Buffer.concat([cipher.update(plain, 'utf8'), cipher.final()]);
  const tag = cipher.getAuthTag();
  return `${ENC_PREFIX}${iv.toString('base64')}:${ciphertext.toString('base64')}:${tag.toString('base64')}`;
}

// Decrypt a stored secret. A value that isn't in the enc:v1: format is returned
// untouched (plaintext row, or encryption never enabled). An encrypted value we
// can't recover (no key, or tampered/garbled) returns '' rather than a broken
// ciphertext, so we never hand an unusable credential to a printer connection.
export function decryptSecret(stored) {
  if (!isEncrypted(stored)) {
    return stored;
  }
  if (!KEY) {
    return '';
  }
  const parts = stored.slice(ENC_PREFIX.length).split(':');
  if (parts.length !== 3) {
    return '';
  }
  try {
    const iv = Buffer.from(parts[0], 'base64');
    const ciphertext = Buffer.from(parts[1], 'base64');
    const tag = Buffer.from(parts[2], 'base64');
    const decipher = createDecipheriv('aes-256-gcm', KEY, iv);
    decipher.setAuthTag(tag);
    return Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString('utf8');
  } catch {
    return '';
  }
}
