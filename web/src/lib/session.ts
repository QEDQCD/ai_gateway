export type ConsoleSession = {
  token: string;
  role: "admin" | "member";
  user_id: string;
  email: string;
  name: string;
  tenant_id?: string;
  expires_at: string;
};

const sessionStorageKey = "ai_gateway_console_session";

type ConsoleSessionSnapshot = Partial<ConsoleSession> | undefined;

function isConsoleRole(role: ConsoleSessionSnapshot["role"]): role is ConsoleSession["role"] {
  return role === "admin" || role === "member";
}

function isConsoleSession(value: unknown): value is ConsoleSession {
  if (!value || typeof value !== "object") {
    return false;
  }

  const session = value as Partial<ConsoleSession>;
  return (
    typeof session.token === "string" &&
    isConsoleRole(session.role) &&
    typeof session.user_id === "string" &&
    typeof session.email === "string" &&
    typeof session.name === "string" &&
    typeof session.expires_at === "string"
  );
}

function readGlobalSession(): ConsoleSessionSnapshot {
  return (globalThis as typeof globalThis & {
    __AI_GATEWAY_CONSOLE_SESSION__?: ConsoleSessionSnapshot;
  }).__AI_GATEWAY_CONSOLE_SESSION__;
}

function readStoredSession(): ConsoleSession | null {
  if (typeof localStorage === "undefined") {
    return null;
  }

  try {
    const raw = localStorage.getItem(sessionStorageKey);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw);
    return isConsoleSession(parsed) ? parsed : null;
  } catch {
    return null;
  }
}

export function getDefaultSession() {
  return null;
}

export function getConsoleSession(): ConsoleSession | null {
  const stored = readStoredSession();
  if (stored) {
    return stored;
  }

  const snapshot = readGlobalSession();
  return isConsoleSession(snapshot) ? snapshot : null;
}

export function saveConsoleSession(session: ConsoleSession) {
  if (typeof localStorage === "undefined") {
    return;
  }

  localStorage.setItem(sessionStorageKey, JSON.stringify(session));
}

export function clearConsoleSession() {
  if (typeof localStorage === "undefined") {
    return;
  }

  localStorage.removeItem(sessionStorageKey);
}

export function useConsoleSession() {
  return getConsoleSession();
}
