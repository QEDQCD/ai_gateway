export type ConsoleSession = {
  role: "admin" | "member";
  tenant_id?: string;
  user_id: string;
};

const defaultSession: ConsoleSession = {
  role: "admin",
  user_id: "user_admin_demo",
};

type ConsoleSessionSnapshot = Partial<ConsoleSession> | undefined;

function isConsoleRole(role: ConsoleSessionSnapshot["role"]): role is ConsoleSession["role"] {
  return role === "admin" || role === "member";
}

function readGlobalSession(): ConsoleSessionSnapshot {
  return (globalThis as typeof globalThis & {
    __AI_GATEWAY_CONSOLE_SESSION__?: ConsoleSessionSnapshot;
  }).__AI_GATEWAY_CONSOLE_SESSION__;
}

export function getDefaultSession(): ConsoleSession {
  return { ...defaultSession };
}

export function getConsoleSession(): ConsoleSession {
  const snapshot = readGlobalSession();

  if (!snapshot || !isConsoleRole(snapshot.role) || typeof snapshot.user_id !== "string") {
    return getDefaultSession();
  }

  return {
    role: snapshot.role,
    user_id: snapshot.user_id,
    tenant_id: snapshot.tenant_id,
  };
}

export function useConsoleSession() {
  return getConsoleSession();
}
