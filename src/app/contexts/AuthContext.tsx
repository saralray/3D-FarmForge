import { createContext, useContext, useEffect, useState, ReactNode } from 'react';
import {
  ADMIN_USERNAME,
  PUBLIC_VIEWER_MODE,
  PUBLIC_VIEWER_USER,
  SLICER_OPERATOR_GRANT_PARAM,
  SLICER_OPERATOR_USER,
} from '../lib/runtimeConfig';
import { generateId } from '../lib/id';
import { logAuditEvent, setAuditActor } from '../lib/auditApi';
import { verifySlicerGrant } from '../lib/slicerGrantApi';
import {
  changeAdminCredential,
  setupAdminCredential,
  verifyAdminCredential,
} from '../lib/adminCredentialApi';

interface User {
  id: string;
  name: string;
  username: string;
  role: UserRole;
}

interface AuthContextType {
  user: User | null;
  login: (username: string, password: string) => Promise<LoginResult>;
  loginAsViewer: () => Promise<LoginResult>;
  setupAdminPassword: (password: string) => Promise<ChangePasswordResult>;
  changeAdminPassword: (
    currentPassword: string,
    newPassword: string,
  ) => Promise<ChangePasswordResult>;
  createUser: (input: CreateUserInput) => Promise<CreateUserResult>;
  removeUser: (userId: string) => Promise<RemoveUserResult>;
  changeUserPassword: (userId: string, password: string) => Promise<ChangePasswordResult>;
  logout: () => void;
  isLoading: boolean;
  users: User[];
}

interface LoginResult {
  success: boolean;
  error?: string;
  lockedUntil?: number;
}

interface CreateUserInput {
  name: string;
  username: string;
  password: string;
  role: UserRole;
}

interface CreateUserResult {
  success: boolean;
  error?: string;
}

interface RemoveUserResult {
  success: boolean;
  error?: string;
}

interface ChangePasswordResult {
  success: boolean;
  error?: string;
}

interface StoredSession {
  user: User;
  expiresAt: number;
}

interface StoredUserRecord extends User {
  passwordHash: string;
}

type UserRole = 'admin' | 'operator' | 'viewer';

const AuthContext = createContext<AuthContextType | undefined>(undefined);

const SESSION_STORAGE_KEY = 'printfarm_session';
const USER_STORAGE_KEY = 'printfarm_users';
const SESSION_DURATION_MS = 8 * 60 * 60 * 1000;

// The admin's password is not stored client-side — it lives server-side and is
// set on first run (see lib/adminCredentialApi.ts), so this record carries no
// usable hash. Admin login is verified against the server, not this value.
const DEFAULT_USERS: StoredUserRecord[] = [
  {
    id: '1',
    username: ADMIN_USERNAME,
    passwordHash: '',
    name: 'Print Farm Admin',
    role: 'admin' as const,
  },
];

const DEFAULT_ADMIN = DEFAULT_USERS[0];

function sanitizeUser(record: StoredUserRecord): User {
  return {
    id: record.id,
    name: record.name,
    username: record.username,
    role: record.role,
  };
}

function readStoredUsers(): StoredUserRecord[] {
  const rawValue = localStorage.getItem(USER_STORAGE_KEY);
  if (!rawValue) {
    localStorage.setItem(USER_STORAGE_KEY, JSON.stringify(DEFAULT_USERS));
    return DEFAULT_USERS;
  }

  try {
    const parsed = JSON.parse(rawValue) as StoredUserRecord[];
    if (!Array.isArray(parsed) || parsed.length === 0) {
      throw new Error('Invalid stored users');
    }

    const validUsers = parsed.filter(
      (candidate) =>
        candidate &&
        typeof candidate.id === 'string' &&
        typeof candidate.name === 'string' &&
        typeof candidate.username === 'string' &&
        typeof candidate.passwordHash === 'string' &&
        ['admin', 'operator', 'viewer'].includes(candidate.role)
    );

    if (validUsers.length === 0) {
      throw new Error('No valid users');
    }

    const nextUsers = validUsers.filter((candidate) => candidate.username !== DEFAULT_ADMIN.username);
    nextUsers.unshift(DEFAULT_ADMIN);
    writeStoredUsers(nextUsers);
    return nextUsers;
  } catch {
    localStorage.setItem(USER_STORAGE_KEY, JSON.stringify(DEFAULT_USERS));
    return DEFAULT_USERS;
  }
}

function writeStoredUsers(users: StoredUserRecord[]) {
  localStorage.setItem(USER_STORAGE_KEY, JSON.stringify(users));
}

function readStoredSession(): StoredSession | null {
  const rawValue = sessionStorage.getItem(SESSION_STORAGE_KEY);
  if (!rawValue) {
    return null;
  }

  try {
    const parsed = JSON.parse(rawValue) as StoredSession;
    if (!parsed.user || typeof parsed.expiresAt !== 'number') {
      sessionStorage.removeItem(SESSION_STORAGE_KEY);
      return null;
    }

    if (parsed.expiresAt <= Date.now()) {
      sessionStorage.removeItem(SESSION_STORAGE_KEY);
      return null;
    }

    return parsed;
  } catch {
    sessionStorage.removeItem(SESSION_STORAGE_KEY);
    return null;
  }
}

function writeStoredSession(user: User | null) {
  if (!user) {
    sessionStorage.removeItem(SESSION_STORAGE_KEY);
    return;
  }

  const session: StoredSession = {
    user,
    expiresAt: Date.now() + SESSION_DURATION_MS,
  };
  sessionStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(session));
}

function createViewerSession() {
  writeStoredSession(PUBLIC_VIEWER_USER);
  return PUBLIC_VIEWER_USER;
}

// When the dashboard is opened from a slicer's "Device" tab, the slicer-proxy
// redirects here with `?slicer_grant=<token>`. Pull the token out and strip the
// param from the URL so it does not linger or get bookmarked. The token itself
// is meaningless until the server verifies its signature, so this only extracts
// it — the caller verifies before granting operator access.
function takeSlicerGrantToken(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }

  const params = new URLSearchParams(window.location.search);
  const token = params.get(SLICER_OPERATOR_GRANT_PARAM);
  if (!token) {
    return null;
  }

  params.delete(SLICER_OPERATOR_GRANT_PARAM);
  const query = params.toString();
  const nextUrl = `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`;
  window.history.replaceState({}, '', nextUrl);
  return token;
}

function sha256Fallback(message: string) {
  const encoder = new TextEncoder();
  const bytes = Array.from(encoder.encode(message));
  const bitLength = bytes.length * 8;

  bytes.push(0x80);
  while ((bytes.length % 64) !== 56) {
    bytes.push(0);
  }

  for (let shift = 56; shift >= 0; shift -= 8) {
    bytes.push((bitLength >>> shift) & 0xff);
  }

  const words = new Uint32Array(64);
  const hash = new Uint32Array([
    0x6a09e667,
    0xbb67ae85,
    0x3c6ef372,
    0xa54ff53a,
    0x510e527f,
    0x9b05688c,
    0x1f83d9ab,
    0x5be0cd19,
  ]);

  const k = new Uint32Array([
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4,
    0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe,
    0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f,
    0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
    0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc,
    0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
    0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116,
    0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
    0xc67178f2,
  ]);

  for (let offset = 0; offset < bytes.length; offset += 64) {
    for (let index = 0; index < 16; index += 1) {
      const base = offset + index * 4;
      words[index] =
        (bytes[base] << 24) |
        (bytes[base + 1] << 16) |
        (bytes[base + 2] << 8) |
        bytes[base + 3];
    }

    for (let index = 16; index < 64; index += 1) {
      const s0 =
        ((words[index - 15] >>> 7) | (words[index - 15] << 25)) ^
        ((words[index - 15] >>> 18) | (words[index - 15] << 14)) ^
        (words[index - 15] >>> 3);
      const s1 =
        ((words[index - 2] >>> 17) | (words[index - 2] << 15)) ^
        ((words[index - 2] >>> 19) | (words[index - 2] << 13)) ^
        (words[index - 2] >>> 10);
      words[index] = (((words[index - 16] + s0) >>> 0) + ((words[index - 7] + s1) >>> 0)) >>> 0;
    }

    let [a, b, c, d, e, f, g, h] = hash;

    for (let index = 0; index < 64; index += 1) {
      const s1 =
        ((e >>> 6) | (e << 26)) ^
        ((e >>> 11) | (e << 21)) ^
        ((e >>> 25) | (e << 7));
      const choice = (e & f) ^ (~e & g);
      const temp1 = (((((h + s1) >>> 0) + choice) >>> 0) + ((k[index] + words[index]) >>> 0)) >>> 0;
      const s0 =
        ((a >>> 2) | (a << 30)) ^
        ((a >>> 13) | (a << 19)) ^
        ((a >>> 22) | (a << 10));
      const majority = (a & b) ^ (a & c) ^ (b & c);
      const temp2 = (s0 + majority) >>> 0;

      h = g;
      g = f;
      f = e;
      e = (d + temp1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (temp1 + temp2) >>> 0;
    }

    hash[0] = (hash[0] + a) >>> 0;
    hash[1] = (hash[1] + b) >>> 0;
    hash[2] = (hash[2] + c) >>> 0;
    hash[3] = (hash[3] + d) >>> 0;
    hash[4] = (hash[4] + e) >>> 0;
    hash[5] = (hash[5] + f) >>> 0;
    hash[6] = (hash[6] + g) >>> 0;
    hash[7] = (hash[7] + h) >>> 0;
  }

  return Array.from(hash)
    .map((value) => value.toString(16).padStart(8, '0'))
    .join('');
}

async function hashPassword(password: string) {
  if (typeof crypto !== 'undefined' && crypto.subtle) {
    const buffer = await crypto.subtle.digest(
      'SHA-256',
      new TextEncoder().encode(password)
    );

    return Array.from(new Uint8Array(buffer))
      .map((value) => value.toString(16).padStart(2, '0'))
      .join('');
  }

  return sha256Fallback(password);
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    if (PUBLIC_VIEWER_MODE) {
      setUsers([]);
      setUser(PUBLIC_VIEWER_USER);
      setIsLoading(false);
      return;
    }

    let cancelled = false;

    const bootstrap = async () => {
      const storedUsers = readStoredUsers();
      if (!cancelled) {
        setUsers(storedUsers.map(sanitizeUser));
      }

      // A slicer "Device" link can grant operator access, but only once the
      // server verifies the signed grant token — a forged or stale token is
      // rejected and the user falls through to a normal session. The token is
      // stripped from the URL up front regardless of the outcome.
      const grantToken = takeSlicerGrantToken();
      if (grantToken && (await verifySlicerGrant(grantToken))) {
        if (!cancelled) {
          setUser(SLICER_OPERATOR_USER);
          writeStoredSession(SLICER_OPERATOR_USER);
          setIsLoading(false);
        }
        return;
      }

      const storedSession = readStoredSession();
      if (!cancelled) {
        setUser(storedSession ? storedSession.user : createViewerSession());
        setIsLoading(false);
      }
    };

    bootstrap();

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!user) {
      return;
    }

    const interval = window.setInterval(() => {
      const storedSession = readStoredSession();
      if (!storedSession) {
        setUser(createViewerSession());
      }
    }, 60 * 1000);

    return () => window.clearInterval(interval);
  }, [user]);

  // Keep the audit logger's notion of "who is acting" in sync with the signed-in
  // user so action sites can attribute entries without threading the user through.
  useEffect(() => {
    setAuditActor(user);
  }, [user]);

  const login = async (username: string, password: string): Promise<LoginResult> => {
    if (PUBLIC_VIEWER_MODE) {
      return { success: true };
    }

    const normalizedUsername = username.trim().toLowerCase();
    const trimmedPassword = password.trim();

    if (!normalizedUsername || !trimmedPassword) {
      return {
        success: false,
        error: 'Enter both username and password.',
      };
    }

    await new Promise((resolve) => setTimeout(resolve, 500));

    const passwordHash = await hashPassword(trimmedPassword);

    // The admin account is verified against the server-stored credential, not a
    // client-side hash. (Operators/viewers remain client-side below.)
    if (normalizedUsername === ADMIN_USERNAME) {
      const valid = await verifyAdminCredential(passwordHash);
      if (!valid) {
        return { success: false, error: 'Invalid credentials.' };
      }
      const userData = sanitizeUser(DEFAULT_ADMIN);
      setUser(userData);
      writeStoredSession(userData);
      setAuditActor(userData);
      logAuditEvent('auth.login', userData.username, { role: userData.role });
      return { success: true };
    }

    const availableUsers = readStoredUsers();
    const foundUser = availableUsers.find(
      (candidate) =>
        candidate.username === normalizedUsername && candidate.passwordHash === passwordHash
    );

    if (foundUser) {
      const userData = {
        id: foundUser.id,
        name: foundUser.name,
        username: foundUser.username,
        role: foundUser.role,
      };
      setUser(userData);
      writeStoredSession(userData);
      // Attribute the login (and any immediate follow-up action) to this user
      // right away, ahead of the actor-sync effect that runs after render.
      setAuditActor(userData);
      logAuditEvent('auth.login', userData.username, { role: userData.role });
      return { success: true };
    }

    return {
      success: false,
      error: 'Invalid credentials.',
    };
  };

  const loginAsViewer = async (): Promise<LoginResult> => {
    const viewerUser = PUBLIC_VIEWER_USER;
    setUser(viewerUser);
    writeStoredSession(viewerUser);
    return { success: true };
  };

  // First-run setup: choose the admin password through the website. Succeeds only
  // while no admin password exists server-side; on success the admin is signed in.
  const setupAdminPassword = async (password: string): Promise<ChangePasswordResult> => {
    if (PUBLIC_VIEWER_MODE) {
      return { success: false, error: 'Admin setup is disabled in public viewer mode.' };
    }

    const trimmedPassword = password.trim();
    if (trimmedPassword.length < 8) {
      return { success: false, error: 'Password must be at least 8 characters.' };
    }

    const passwordHash = await hashPassword(trimmedPassword);
    const result = await setupAdminCredential(passwordHash);
    if (!result.ok) {
      return { success: false, error: result.error ?? 'Unable to set the admin password.' };
    }

    const userData = sanitizeUser(DEFAULT_ADMIN);
    setUser(userData);
    writeStoredSession(userData);
    setAuditActor(userData);
    logAuditEvent('admin.password_setup', userData.username);
    return { success: true };
  };

  // Change the admin password. The server requires the current password to
  // authorize the change, so it is collected and sent (hashed) alongside the new.
  const changeAdminPassword = async (
    currentPassword: string,
    newPassword: string,
  ): Promise<ChangePasswordResult> => {
    if (PUBLIC_VIEWER_MODE) {
      return { success: false, error: 'User management is disabled in public viewer mode.' };
    }

    if (!user || user.username !== ADMIN_USERNAME) {
      return { success: false, error: 'Only the admin account can change the admin password.' };
    }

    const trimmedCurrent = currentPassword.trim();
    const trimmedNew = newPassword.trim();
    if (!trimmedCurrent) {
      return { success: false, error: 'Enter your current password.' };
    }
    if (trimmedNew.length < 8) {
      return { success: false, error: 'Password must be at least 8 characters.' };
    }

    const [currentHash, newHash] = await Promise.all([
      hashPassword(trimmedCurrent),
      hashPassword(trimmedNew),
    ]);
    const result = await changeAdminCredential(currentHash, newHash);
    if (!result.ok) {
      return { success: false, error: result.error ?? 'Unable to change password.' };
    }

    logAuditEvent('user.password_change', user.username);
    return { success: true };
  };

  const createUser = async ({
    name,
    username,
    password,
    role,
  }: CreateUserInput): Promise<CreateUserResult> => {
    if (PUBLIC_VIEWER_MODE) {
      return {
        success: false,
        error: 'User management is disabled in public viewer mode.',
      };
    }

    if (!user || user.role !== 'admin') {
      return {
        success: false,
        error: 'Only admins can add users.',
      };
    }

    const normalizedName = name.trim();
    const normalizedUsername = username.trim().toLowerCase();
    const trimmedPassword = password.trim();

    if (!normalizedName || !normalizedUsername || !trimmedPassword) {
      return {
        success: false,
        error: 'Name, username, and password are required.',
      };
    }

    if (trimmedPassword.length < 8) {
      return {
        success: false,
        error: 'Password must be at least 8 characters.',
      };
    }

    const availableUsers = readStoredUsers();
    if (availableUsers.some((candidate) => candidate.username === normalizedUsername)) {
      return {
        success: false,
        error: 'That username is already in use.',
      };
    }

    const passwordHash = await hashPassword(trimmedPassword);
    const nextUser: StoredUserRecord = {
      id: generateId(),
      name: normalizedName,
      username: normalizedUsername,
      passwordHash,
      role,
    };

    const nextUsers = [...availableUsers, nextUser];
    writeStoredUsers(nextUsers);
    setUsers(nextUsers.map(sanitizeUser));

    logAuditEvent('user.create', normalizedUsername, { role });

    return { success: true };
  };

  const removeUser = async (userId: string): Promise<RemoveUserResult> => {
    if (PUBLIC_VIEWER_MODE) {
      return {
        success: false,
        error: 'User management is disabled in public viewer mode.',
      };
    }

    if (!user || user.role !== 'admin') {
      return {
        success: false,
        error: 'Only admins can remove users.',
      };
    }

    if (!userId) {
      return {
        success: false,
        error: 'User id is required.',
      };
    }

    if (user.id === userId) {
      return {
        success: false,
        error: 'You cannot remove your own account.',
      };
    }

    const availableUsers = readStoredUsers();
    const targetUser = availableUsers.find((candidate) => candidate.id === userId);

    if (!targetUser) {
      return {
        success: false,
        error: 'User not found.',
      };
    }

    const adminCount = availableUsers.filter((candidate) => candidate.role === 'admin').length;
    if (targetUser.role === 'admin' && adminCount <= 1) {
      return {
        success: false,
        error: 'At least one admin account must remain.',
      };
    }

    const nextUsers = availableUsers.filter((candidate) => candidate.id !== userId);
    writeStoredUsers(nextUsers);
    setUsers(nextUsers.map(sanitizeUser));

    logAuditEvent('user.delete', targetUser.username, { role: targetUser.role });

    return { success: true };
  };

  const changeUserPassword = async (
    userId: string,
    password: string,
  ): Promise<ChangePasswordResult> => {
    if (PUBLIC_VIEWER_MODE) {
      return {
        success: false,
        error: 'User management is disabled in public viewer mode.',
      };
    }

    if (!user) {
      return {
        success: false,
        error: 'You must be signed in to change your password.',
      };
    }

    if (user.id !== userId) {
      return {
        success: false,
        error: 'You can only change your own password.',
      };
    }

    const trimmedPassword = password.trim();
    if (!userId || !trimmedPassword) {
      return {
        success: false,
        error: 'User and password are required.',
      };
    }

    const availableUsers = readStoredUsers();
    const targetIndex = availableUsers.findIndex((candidate) => candidate.id === userId);

    if (targetIndex === -1) {
      return {
        success: false,
        error: 'User not found.',
      };
    }

    const passwordHash = await hashPassword(trimmedPassword);
    const nextUsers = [...availableUsers];
    nextUsers[targetIndex] = {
      ...nextUsers[targetIndex],
      passwordHash,
    };

    writeStoredUsers(nextUsers);
    setUsers(nextUsers.map(sanitizeUser));

    logAuditEvent('user.password_change', nextUsers[targetIndex].username);

    return { success: true };
  };

  const logout = () => {
    if (PUBLIC_VIEWER_MODE) {
      return;
    }

    // Log before swapping to the viewer session so the entry is attributed to the
    // user who is signing out, not the anonymous viewer.
    if (user && user.role !== 'viewer') {
      logAuditEvent('auth.logout', user.username, { role: user.role });
    }

    const viewerUser = createViewerSession();
    setUser(viewerUser);
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        users,
        login,
        loginAsViewer,
        setupAdminPassword,
        changeAdminPassword,
        createUser,
        removeUser,
        changeUserPassword,
        logout,
        isLoading,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
