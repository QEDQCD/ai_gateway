import { clearConsoleSession, getConsoleSession, type ConsoleSession } from "./session";

export type KeyMetric = {
  label: string;
  value: string;
};

export type TableRow = {
  columns: string[];
};

export type OverviewPageData = {
  stats: KeyMetric[];
  platform_metrics: KeyMetric[];
  tenant_posture: TableRow[];
  route_health: TableRow[];
  top_models: TableRow[];
  recent_alerts: TableRow[];
  audit_snapshot: TableRow[];
  quota_summary?: TenantQuotaSummary;
};

export type TenantQuotaSummary = {
  configured?: boolean;
  request_limit: number;
  requests_used: number;
  requests_remaining: number;
  token_limit: number;
  tokens_used: number;
  tokens_remaining: number;
  period_start?: string;
  period_end?: string;
  resets_at: string;
};

export type ConsoleSystemStatus = {
  console_stage: string;
  run_mode: string;
  gateway_health: string;
  quota_protection: string;
  console_entry: string;
  gateway_admin_api: string;
  internal_services: string[];
  hidden_modules: string[];
};

export type APIKeyItem = {
  id: string;
  name: string;
  tenant: string;
  status: string;
  scopes: APIKeyScope[];
  last_used_at: string;
  owner_user_id?: string;
  created_by_user_id?: string;
  expires_at?: string;
  revealable?: boolean;
  legacy_unrecoverable?: boolean;
};

export type APIKeysPageData = {
  items: APIKeyItem[];
  credential_mode?: string;
};

export type APIKeyScope = "chat" | "rag" | "embeddings";

export type APIKeyMutationResult = {
  item: APIKeyItem;
  raw_key?: string;
};

export type APIKeySecretView = {
  api_key_id: string;
  masked_key: string;
  full_key?: string;
  revealable: boolean;
  legacy_unrecoverable: boolean;
  expires_at?: string;
};

export type ApplicationItem = {
  id: string;
  email: string;
  name: string;
  company_name: string;
  use_case: string;
  status: string;
  created_at: string;
};

export type ApplicationsPageData = {
  items: ApplicationItem[];
};

export type ApproveApplicationPayload = {
  actor_id: string;
  comment: string;
  tenant_id: string;
  token_limit: number;
  allowed_models: string[];
};

export type RejectApplicationPayload = {
  actor_id: string;
  comment: string;
};

export type ApplicationMutationResult = {
  item: ApplicationItem;
};

export type AccountDeletionApplicationItem = {
  id: string;
  user_id: string;
  tenant_id: string;
  user_email: string;
  user_name: string;
  reason: string;
  status: string;
  disabled_api_keys: number;
  created_at: string;
  reviewed_at?: string;
};

export type AccountDeletionApplicationsPageData = {
  items: AccountDeletionApplicationItem[];
};

export type CreateAccountDeletionApplicationPayload = {
  reason: string;
};

export type ReviewAccountDeletionApplicationPayload = {
  actor_id: string;
  comment: string;
};

export type AccountDeletionApplicationMutationResult = {
  item: AccountDeletionApplicationItem;
};

export type RouteMetric = {
  label: string;
  value: string;
};

export type RouteItem = {
  requested_model: string;
  route_label: string;
  credential: string;
  latency: string;
  status: string;
  provider_group: "qwen" | "mimo" | "other";
};

export type RoutesPageData = {
  stats: RouteMetric[];
  items: RouteItem[];
  policy_summary: string[];
};

export type PlaygroundRunResponse = {
  route_label: string;
  endpoint: string;
  latency: string;
  status: string;
  response: string;
  platform_key: string;
};

export type PlaygroundPageData = {
  available_models: string[];
  last_run?: PlaygroundRunResponse | null;
};

export type PlaygroundRunRequest = {
  model: string;
  prompt: string;
};

export type AuditSummary = {
  title: string;
  content: string;
};

export type AuditMetric = {
  label: string;
  value: string;
};

export type AuditEvent = {
  time: string;
  type: string;
  status: string;
  detail: string;
};

export type AuditItem = {
  time: string;
  tenant: string;
  endpoint: string;
  request_model: string;
  upstream_model: string;
  resolved_model: string;
  task_class: string;
  routing_reason: string;
  target_model_tier: string;
  status: string;
  route_label: string;
  latency: string;
  usage_source: string;
  total_cost: string;
};

export type AuditPageData = {
  metrics: AuditMetric[];
  events: AuditEvent[];
  items: AuditItem[];
  summaries: AuditSummary[];
};

export type PricingModelItem = {
  model: string;
  input_price: string;
  output_price: string;
  cached_price: string;
};

export type UsageOverviewData = {
  total_requests: number;
  success_rate: string;
  total_tokens: string;
  input_tokens: string;
  output_tokens: string;
  cached_tokens: string;
  average_latency: string;
  estimated_share: string;
  input_cost: string;
  output_cost: string;
  cached_cost: string;
  total_cost: string;
  pricing_models: PricingModelItem[];
};

export type UsageTrendPoint = {
  label: string;
  value: string;
};

export type UsageTrendData = {
  requests: UsageTrendPoint[];
  tokens: UsageTrendPoint[];
  success: UsageTrendPoint[];
  costs: UsageTrendPoint[];
};

export type UsageLatencyCell = {
  bucket_label: string;
  latency: string;
  status: string;
  requests: string;
};

export type UsageLatencyLane = {
  model: string;
  route_label: string;
  success_rate: string;
  average_latency: string;
  cells: UsageLatencyCell[];
};

export type UsageLatencyWallData = {
  window_label: string;
  buckets: string[];
  lanes: UsageLatencyLane[];
};

export type UsageFailureBucket = {
  label: string;
  value: string;
};

export type UsageFailureData = {
  breakdown: UsageFailureBucket[];
  recent_events: string[];
};

export type UsageRequestItem = {
  request_id: string;
  tenant: string;
  endpoint: string;
  model: string;
  resolved_model: string;
  task_class: string;
  routing_reason: string;
  target_model_tier: string;
  status: string;
  total_tokens: string;
  input_tokens: string;
  output_tokens: string;
  cached_tokens: string;
  latency: string;
  usage_source: string;
  input_cost: string;
  output_cost: string;
  cached_cost: string;
  total_cost: string;
  input_price: string;
  output_price: string;
  cached_price: string;
};

export type UsageRequestsPageData = {
  items: UsageRequestItem[];
  total: number;
  limit: number;
  offset: number;
};

export type UsageRequestsQuery = {
  limit: number;
  offset: number;
};

export type MemberOverviewPageData = {
  tenant_id: string;
  tenant_name: string;
  active_api_keys: number;
  quota?: TenantQuotaSummary;
};

export type MemberAuditItem = {
  time: string;
  event_type: string;
  target_type: string;
  target_id: string;
  detail: string;
};

export type MemberAuditPageData = {
  items: MemberAuditItem[];
};

export type ConsoleLoginRequest = {
  email: string;
  password: string;
};

export type CreateApplicationPayload = {
  email: string;
  name: string;
  company_name: string;
  use_case: string;
  password: string;
  captcha_pass_token: string;
};

export type CaptchaChallenge = {
  captcha_id: string;
  image_data: string;
  expires_at: string;
};

export type VerifyCaptchaPayload = {
  captcha_id: string;
  captcha_code: string;
};

export type CaptchaPassResult = {
  captcha_pass_token: string;
  expires_at: string;
};

type JsonRecord = Record<string, unknown>;

function asRecord(value: unknown): JsonRecord {
  return value && typeof value === "object" ? (value as JsonRecord) : {};
}

function readString(record: JsonRecord, key: string) {
  const value = record[key];
  return typeof value === "string" ? value : "";
}

function readNumber(record: JsonRecord, key: string) {
  const value = record[key];
  return typeof value === "number" ? value : 0;
}

function readStringArray(record: JsonRecord, key: string) {
  const value = record[key];
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function readRouteLabel(record: JsonRecord) {
  return readString(record, "route_label");
}

function toPricingModelItem(value: unknown): PricingModelItem {
  const record = asRecord(value);

  return {
    model: readString(record, "model"),
    input_price: readString(record, "input_price"),
    output_price: readString(record, "output_price"),
    cached_price: readString(record, "cached_price"),
  };
}

function toUsageOverviewData(value: unknown): UsageOverviewData {
  const record = asRecord(value);

  return {
    total_requests: readNumber(record, "total_requests"),
    success_rate: readString(record, "success_rate"),
    total_tokens: readString(record, "total_tokens"),
    input_tokens: readString(record, "input_tokens"),
    output_tokens: readString(record, "output_tokens"),
    cached_tokens: readString(record, "cached_tokens"),
    average_latency: readString(record, "average_latency"),
    estimated_share: readString(record, "estimated_share"),
    input_cost: readString(record, "input_cost"),
    output_cost: readString(record, "output_cost"),
    cached_cost: readString(record, "cached_cost"),
    total_cost: readString(record, "total_cost"),
    pricing_models: Array.isArray(record.pricing_models)
      ? record.pricing_models.map(toPricingModelItem)
      : [],
  };
}

function toUsageTrendPoint(value: unknown): UsageTrendPoint {
  const record = asRecord(value);

  return {
    label: readString(record, "label"),
    value: readString(record, "value"),
  };
}

function toUsageTrendData(value: unknown): UsageTrendData {
  const record = asRecord(value);

  return {
    requests: Array.isArray(record.requests) ? record.requests.map(toUsageTrendPoint) : [],
    tokens: Array.isArray(record.tokens) ? record.tokens.map(toUsageTrendPoint) : [],
    success: Array.isArray(record.success) ? record.success.map(toUsageTrendPoint) : [],
    costs: Array.isArray(record.costs) ? record.costs.map(toUsageTrendPoint) : [],
  };
}

function toUsageRequestItem(value: unknown): UsageRequestItem {
  const record = asRecord(value);

  return {
    request_id: readString(record, "request_id"),
    tenant: readString(record, "tenant"),
    endpoint: readString(record, "endpoint"),
    model: readString(record, "model"),
    resolved_model: readString(record, "resolved_model"),
    task_class: readString(record, "task_class"),
    routing_reason: readString(record, "routing_reason"),
    target_model_tier: readString(record, "target_model_tier"),
    status: readString(record, "status"),
    total_tokens: readString(record, "total_tokens"),
    input_tokens: readString(record, "input_tokens"),
    output_tokens: readString(record, "output_tokens"),
    cached_tokens: readString(record, "cached_tokens"),
    latency: readString(record, "latency"),
    usage_source: readString(record, "usage_source"),
    input_cost: readString(record, "input_cost"),
    output_cost: readString(record, "output_cost"),
    cached_cost: readString(record, "cached_cost"),
    total_cost: readString(record, "total_cost"),
    input_price: readString(record, "input_price"),
    output_price: readString(record, "output_price"),
    cached_price: readString(record, "cached_price"),
  };
}

function toUsageRequestsPageData(value: unknown): UsageRequestsPageData {
  const record = asRecord(value);

  return {
    items: Array.isArray(record.items) ? record.items.map(toUsageRequestItem) : [],
    total: readNumber(record, "total"),
    limit: readNumber(record, "limit"),
    offset: readNumber(record, "offset"),
  };
}

function toRouteItem(value: unknown): RouteItem {
  const record = asRecord(value);
  const providerGroup = readString(record, "provider_group");

  return {
    requested_model: readString(record, "requested_model"),
    route_label: readRouteLabel(record),
    credential: readString(record, "credential"),
    latency: readString(record, "latency"),
    status: readString(record, "status"),
    provider_group:
      providerGroup === "qwen" || providerGroup === "mimo" || providerGroup === "other"
        ? providerGroup
        : "other",
  };
}

function toPlaygroundRun(value: unknown): PlaygroundRunResponse {
  const record = asRecord(value);

  return {
    route_label: readRouteLabel(record),
    endpoint: readString(record, "endpoint"),
    latency: readString(record, "latency"),
    status: readString(record, "status"),
    response: readString(record, "response"),
    platform_key: readString(record, "platform_key"),
  };
}

async function requestJson<T>(path: string, init?: RequestInit): Promise<T> {
  const session = getConsoleSession();
  let response: Response;

  if (session?.token) {
    const headers = new Headers(init?.headers);
    headers.set("X-Console-Session", session.token);
    response = await fetch(path, {
      ...(init ?? {}),
      headers: Object.fromEntries(headers.entries()),
    });
  } else if (init) {
    response = await fetch(path, init);
  } else {
    response = await fetch(path);
  }

  if (!response.ok) {
    let detail = "";

    try {
      detail = await response.text();
    } catch {
      detail = "";
    }

    if (response.status === 401 && path !== "/api/console/session/login") {
      clearConsoleSession();
      if (typeof window !== "undefined") {
        window.location.assign("/login");
      }
    }

    const suffix = detail ? `：${detail}` : "";
    throw new Error(`请求失败（${response.status}）${suffix}`);
  }

  return (await response.json()) as T;
}

export function getOverview() {
  return requestJson<OverviewPageData>("/api/admin/overview");
}

export function getSystemStatus() {
  return requestJson<ConsoleSystemStatus>("/api/admin/system/status");
}

export function getAPIKeys() {
  return requestJson<APIKeysPageData>("/api/admin/api-keys");
}

export function getApplications() {
  return requestJson<ApplicationsPageData>("/api/admin/applications");
}

export function approveApplication(id: string, payload: ApproveApplicationPayload) {
  return requestJson<ApplicationMutationResult>(`/api/admin/applications/${id}/approve`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function rejectApplication(id: string, payload: RejectApplicationPayload) {
  return requestJson<ApplicationMutationResult>(`/api/admin/applications/${id}/reject`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function getAccountDeletionApplications() {
  return requestJson<AccountDeletionApplicationsPageData>("/api/admin/account-deletion-applications");
}

export function approveAccountDeletionApplication(
  id: string,
  payload: ReviewAccountDeletionApplicationPayload,
) {
  return requestJson<AccountDeletionApplicationMutationResult>(
    `/api/admin/account-deletion-applications/${id}/approve`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    },
  );
}

export function rejectAccountDeletionApplication(
  id: string,
  payload: ReviewAccountDeletionApplicationPayload,
) {
  return requestJson<AccountDeletionApplicationMutationResult>(
    `/api/admin/account-deletion-applications/${id}/reject`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    },
  );
}

export type CreateAPIKeyPayload = {
  tenant_id: string;
  name: string;
  scopes: APIKeyScope[];
};

export type RotateAPIKeyPayload = {
  name?: string;
  scopes?: APIKeyScope[];
};

export function createAPIKey(payload: CreateAPIKeyPayload) {
  return requestJson<APIKeyMutationResult>("/api/admin/api-keys", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function rotateAPIKey(id: string, payload: RotateAPIKeyPayload) {
  return requestJson<APIKeyMutationResult>(`/api/admin/api-keys/${id}/rotate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function deactivateAPIKey(id: string) {
  return requestJson<APIKeyMutationResult>(`/api/admin/api-keys/${id}/deactivate`, {
    method: "POST",
  });
}

export function deleteAPIKey(id: string) {
  return requestJson<APIKeyMutationResult>(`/api/admin/api-keys/${id}`, {
    method: "DELETE",
  });
}

export function revealAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/api/admin/api-keys/${id}/secret`);
}

export function copyAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/api/admin/api-keys/${id}/secret/copy`, {
    method: "POST",
  });
}

export type CreateMemberAPIKeyPayload = {
  name: string;
  scopes: APIKeyScope[];
};

export function getMemberOverview() {
  return requestJson<MemberOverviewPageData>("/api/me/overview");
}

export function getMemberAPIKeys() {
  return requestJson<APIKeysPageData>("/api/me/api-keys");
}

export function createMemberAPIKey(payload: CreateMemberAPIKeyPayload) {
  return requestJson<APIKeyMutationResult>("/api/me/api-keys", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function rotateMemberAPIKey(id: string, payload: RotateAPIKeyPayload) {
  return requestJson<APIKeyMutationResult>(`/api/me/api-keys/${id}/rotate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function deactivateMemberAPIKey(id: string) {
  return requestJson<APIKeyMutationResult>(`/api/me/api-keys/${id}/deactivate`, {
    method: "POST",
  });
}

export function revealMemberAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/api/me/api-keys/${id}/secret`);
}

export function copyMemberAPIKeySecret(id: string) {
  return requestJson<APIKeySecretView>(`/api/me/api-keys/${id}/secret/copy`, {
    method: "POST",
  });
}

export function createAccountDeletionApplication(payload: CreateAccountDeletionApplicationPayload) {
  return requestJson<AccountDeletionApplicationMutationResult>(
    "/api/me/account-deletion-applications",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    },
  );
}

export function getRoutes() {
  return requestJson<JsonRecord>("/api/admin/routes").then((data) => ({
    stats: Array.isArray(data.stats) ? (data.stats as RouteMetric[]) : [],
    items: Array.isArray(data.items) ? data.items.map(toRouteItem) : [],
    policy_summary: readStringArray(data, "policy_summary"),
  }));
}

export function getPlayground() {
  return requestJson<JsonRecord>("/api/admin/playground").then((data) => ({
    available_models: readStringArray(data, "available_models"),
    last_run: data.last_run == null ? null : toPlaygroundRun(data.last_run),
  }));
}

export function runPlayground(payload: PlaygroundRunRequest) {
  return requestJson<JsonRecord>("/api/admin/playground/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  }).then(toPlaygroundRun);
}

export function getAudit() {
  return requestJson<JsonRecord>("/api/admin/audit").then((data) => ({
    metrics: Array.isArray(data.metrics) ? (data.metrics as AuditMetric[]) : [],
    events: Array.isArray(data.events) ? (data.events as AuditEvent[]) : [],
    items: Array.isArray(data.items)
      ? data.items.map((value) => {
          const record = asRecord(value);
          return {
            time: readString(record, "time"),
            tenant: readString(record, "tenant"),
            endpoint: readString(record, "endpoint"),
            request_model: readString(record, "request_model"),
            upstream_model: readString(record, "upstream_model"),
            resolved_model: readString(record, "resolved_model"),
            task_class: readString(record, "task_class"),
            routing_reason: readString(record, "routing_reason"),
            target_model_tier: readString(record, "target_model_tier"),
            status: readString(record, "status"),
            route_label: readRouteLabel(record),
            latency: readString(record, "latency"),
            usage_source: readString(record, "usage_source"),
            total_cost: readString(record, "total_cost"),
          };
        })
      : [],
    summaries: Array.isArray(data.summaries) ? (data.summaries as AuditSummary[]) : [],
  }));
}

export function getUsageOverview(window: "6h" | "24h" | "7d" = "24h") {
  return requestJson<JsonRecord>(`/api/admin/usage/overview?window=${window}`).then(toUsageOverviewData);
}

export function getUsageTrends(window: "6h" | "24h" | "7d" = "24h") {
  return requestJson<JsonRecord>(`/api/admin/usage/trends?window=${window}`).then(toUsageTrendData);
}

export function getUsageLatencyWall(window: "6h" | "24h" | "7d" = "24h") {
  return requestJson<JsonRecord>(`/api/admin/usage/latency-wall?window=${window}`).then((data) => ({
    window_label: readString(data, "window_label"),
    buckets: readStringArray(data, "buckets"),
    lanes: Array.isArray(data.lanes)
      ? data.lanes.map((value) => {
          const record = asRecord(value);
          return {
            model: readString(record, "model"),
            route_label: readRouteLabel(record),
            success_rate: readString(record, "success_rate"),
            average_latency: readString(record, "average_latency"),
            cells: Array.isArray(record.cells)
              ? record.cells.map((cellValue) => {
                  const cell = asRecord(cellValue);
                  return {
                    bucket_label: readString(cell, "bucket_label"),
                    latency: readString(cell, "latency"),
                    status: readString(cell, "status"),
                    requests: readString(cell, "requests"),
                  };
                })
              : [],
          };
        })
      : [],
  }));
}

export function getUsageFailures(window: "6h" | "24h" | "7d" = "24h") {
  return requestJson<UsageFailureData>(`/api/admin/usage/failures?window=${window}`);
}

function createUsageRequestsSearch(query: UsageRequestsQuery) {
  return new URLSearchParams({
    limit: String(query.limit),
    offset: String(query.offset),
  }).toString();
}

export function getUsageRequests(query: UsageRequestsQuery) {
  return requestJson<JsonRecord>(`/api/admin/usage/requests?${createUsageRequestsSearch(query)}`).then(
    toUsageRequestsPageData,
  );
}

export function getMemberUsageOverview() {
  return requestJson<JsonRecord>("/api/me/usage/overview").then(toUsageOverviewData);
}

export function getMemberUsageRequests(query: UsageRequestsQuery) {
  return requestJson<JsonRecord>(`/api/me/usage/requests?${createUsageRequestsSearch(query)}`).then(
    toUsageRequestsPageData,
  );
}

export function getMemberFailures() {
  return requestJson<UsageFailureData>("/api/me/failures");
}

export function getMemberAuditEvents() {
  return requestJson<MemberAuditPageData>("/api/me/audit-events");
}

export function loginConsole(payload: ConsoleLoginRequest) {
  return requestJson<ConsoleSession>("/api/console/session/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function issueCaptcha() {
  return requestJson<CaptchaChallenge>("/api/console/captcha");
}

export function verifyCaptcha(payload: VerifyCaptchaPayload) {
  return requestJson<CaptchaPassResult>("/api/console/captcha/verify", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function createApplication(payload: CreateApplicationPayload) {
  return requestJson<ApplicationMutationResult>("/api/console/applications", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}
