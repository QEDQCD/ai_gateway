export type KeyMetric = {
  label: string;
  value: string;
};

export type TableRow = {
  columns: string[];
};

export type OverviewPageData = {
  stats: KeyMetric[];
  route_health: TableRow[];
  top_models: TableRow[];
  recent_alerts: TableRow[];
  audit_snapshot: TableRow[];
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
};

export type ApplicationMutationResult = {
  item: ApplicationItem;
};

export type RouteMetric = {
  label: string;
  value: string;
};

export type RouteItem = {
  requested_model: string;
  resolved_provider: string;
  credential: string;
  latency: string;
  status: string;
};

export type RoutesPageData = {
  stats: RouteMetric[];
  items: RouteItem[];
  policy_summary: string[];
};

export type PlaygroundRunResponse = {
  resolved_provider: string;
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

export type KnowledgeBaseMetric = {
  label: string;
  value: string;
};

export type KnowledgeBaseItem = {
  name: string;
  documents: string;
  status: string;
  updated_at: string;
};

export type KnowledgeBasesPageData = {
  stats: KnowledgeBaseMetric[];
  items: KnowledgeBaseItem[];
  flow_summary: string[];
  queue_summary: string[];
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
  status: string;
  provider: string;
  latency: string;
  usage_source: string;
};

export type AuditPageData = {
  metrics: AuditMetric[];
  events: AuditEvent[];
  items: AuditItem[];
  summaries: AuditSummary[];
};

export type UsageOverviewData = {
  total_requests: number;
  success_rate: string;
  total_tokens: string;
  average_latency: string;
  estimated_share: string;
};

export type UsageTrendPoint = {
  label: string;
  value: string;
};

export type UsageTrendData = {
  requests: UsageTrendPoint[];
  tokens: UsageTrendPoint[];
  success: UsageTrendPoint[];
};

export type UsageLatencyCell = {
  bucket_label: string;
  latency: string;
  status: string;
  requests: string;
};

export type UsageLatencyLane = {
  model: string;
  provider: string;
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
  status: string;
  total_tokens: string;
  latency: string;
  usage_source: string;
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

async function requestJson<T>(path: string, init?: RequestInit): Promise<T> {
  const response = init ? await fetch(path, init) : await fetch(path);

  if (!response.ok) {
    let detail = "";

    try {
      detail = await response.text();
    } catch {
      detail = "";
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

export type CreateMemberAPIKeyPayload = {
  name: string;
  scopes: APIKeyScope[];
};

export function getMemberOverview() {
  return requestJson<MemberOverviewPageData>("/me/overview");
}

export function getMemberAPIKeys() {
  return requestJson<APIKeysPageData>("/me/api-keys");
}

export function createMemberAPIKey(payload: CreateMemberAPIKeyPayload) {
  return requestJson<APIKeyMutationResult>("/me/api-keys", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function rotateMemberAPIKey(id: string, payload: RotateAPIKeyPayload) {
  return requestJson<APIKeyMutationResult>(`/me/api-keys/${id}/rotate`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function deactivateMemberAPIKey(id: string) {
  return requestJson<APIKeyMutationResult>(`/me/api-keys/${id}/deactivate`, {
    method: "POST",
  });
}

export function getRoutes() {
  return requestJson<RoutesPageData>("/api/admin/routes");
}

export function getPlayground() {
  return requestJson<PlaygroundPageData>("/api/admin/playground");
}

export function runPlayground(payload: PlaygroundRunRequest) {
  return requestJson<PlaygroundRunResponse>("/api/admin/playground/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

export function getKnowledgeBases() {
  return requestJson<KnowledgeBasesPageData>("/api/admin/knowledge-bases");
}

export function getAudit() {
  return requestJson<AuditPageData>("/api/admin/audit");
}

export function getUsageOverview() {
  return requestJson<UsageOverviewData>("/api/admin/usage/overview");
}

export function getUsageTrends() {
  return requestJson<UsageTrendData>("/api/admin/usage/trends");
}

export function getUsageLatencyWall(window: "6h" | "24h" | "7d" = "24h") {
  return requestJson<UsageLatencyWallData>(`/api/admin/usage/latency-wall?window=${window}`);
}

export function getUsageFailures() {
  return requestJson<UsageFailureData>("/api/admin/usage/failures");
}

function createUsageRequestsSearch(query: UsageRequestsQuery) {
  return new URLSearchParams({
    limit: String(query.limit),
    offset: String(query.offset),
  }).toString();
}

export function getUsageRequests(query: UsageRequestsQuery) {
  return requestJson<UsageRequestsPageData>(
    `/api/admin/usage/requests?${createUsageRequestsSearch(query)}`,
  );
}

export function getMemberUsageOverview() {
  return requestJson<UsageOverviewData>("/me/usage/overview");
}

export function getMemberUsageRequests(query: UsageRequestsQuery) {
  return requestJson<UsageRequestsPageData>(`/me/usage/requests?${createUsageRequestsSearch(query)}`);
}

export function getMemberFailures() {
  return requestJson<UsageFailureData>("/me/failures");
}

export function getMemberAuditEvents() {
  return requestJson<MemberAuditPageData>("/me/audit-events");
}
