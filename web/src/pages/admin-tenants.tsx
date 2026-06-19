import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { DataTable, ErrorSection, LoadingSection, StatCard } from "../components/console";
import { getAPIKeys, getOverview, getTenantBilling, getUsageOverview, getUserPointsOverview } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

type TenantSummary = {
  tenant: string;
  keyCount: number;
  activeKeyCount: number;
  scopes: string;
  sampleKeyName: string;
};

const numberFormatter = new Intl.NumberFormat("zh-CN");

function currentMonthValue() {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  return `${year}-${month}`;
}

function formatNumber(value: number | undefined) {
  return numberFormatter.format(value ?? 0);
}

function formatValue(value: string | undefined) {
  return value || "-";
}

function formatTokenCost(tokens: string, cost: string) {
  return `${formatValue(tokens)} / ${formatValue(cost)}`;
}

function buildTenantSummaries(items: Awaited<ReturnType<typeof getAPIKeys>>["items"]): TenantSummary[] {
  const map = new Map<string, TenantSummary>();

  items.forEach((item) => {
    const current = map.get(item.tenant) ?? {
      tenant: item.tenant,
      keyCount: 0,
      activeKeyCount: 0,
      scopes: "",
      sampleKeyName: item.name,
    };
    const scopes = new Set(
      `${current.scopes},${item.scopes.join(",")}`
        .split(",")
        .map((value) => value.trim())
        .filter(Boolean),
    );

    current.keyCount += 1;
    current.activeKeyCount += item.status === "启用" ? 1 : 0;
    current.scopes = Array.from(scopes).join(", ");
    current.sampleKeyName = item.name;
    map.set(item.tenant, current);
  });

  return Array.from(map.values()).sort((left, right) => left.tenant.localeCompare(right.tenant));
}

function findOverviewStat(
  stats: Awaited<ReturnType<typeof getOverview>>["stats"],
  label: string,
  fallback: string,
) {
  return stats.find((item) => item.label === label)?.value ?? fallback;
}

function isHTMLElement(value: unknown): value is HTMLElement {
  return typeof HTMLElement !== "undefined" && value instanceof HTMLElement;
}

function getScrollableAncestor(target: HTMLElement) {
  let current: HTMLElement | null = target.parentElement;

  while (current) {
    const styles = window.getComputedStyle(current);
    const overflowY = styles.overflowY;
    const canScroll = /(auto|scroll|overlay)/.test(overflowY) && current.scrollHeight > current.clientHeight + 8;

    if (canScroll) {
      return current;
    }

    current = current.parentElement;
  }

  const scrollingElement = document.scrollingElement;
  return isHTMLElement(scrollingElement) ? scrollingElement : null;
}

function scrollSectionIntoView(target: HTMLElement | null) {
  if (!target) {
    return;
  }

  const scrollContainer = getScrollableAncestor(target);

  if (scrollContainer) {
    const containerTop = scrollContainer.getBoundingClientRect().top;
    const targetTop = target.getBoundingClientRect().top;
    const top = scrollContainer.scrollTop + (targetTop - containerTop) - 12;

    if (typeof scrollContainer.scrollTo === "function") {
      try {
        scrollContainer.scrollTo({
          top: Math.max(top, 0),
          behavior: "auto",
        });
      } catch {
        // jsdom 不支持 scrollTo，保留后续 fallback。
      }
    } else {
      scrollContainer.scrollTop = Math.max(top, 0);
    }
  }

  if (typeof target.scrollIntoView === "function") {
    try {
      target.scrollIntoView({
        behavior: "auto",
        block: "start",
      });
    } catch {
      // 某些测试环境不支持 scrollIntoView。
    }
  }

  if (!scrollContainer && document.scrollingElement) {
    const absoluteTop = window.scrollY + target.getBoundingClientRect().top - 12;
    document.scrollingElement.scrollTop = Math.max(absoluteTop, 0);
  }
}

function scheduleScrollIntoView(target: HTMLElement | null) {
  if (!target) {
    return () => {};
  }

  let cancelled = false;
  const timeoutIds: number[] = [];
  const rafIds: number[] = [];

  const run = () => {
    if (cancelled) {
      return;
    }

    scrollSectionIntoView(target);
  };

  run();

  if (typeof window.requestAnimationFrame === "function") {
    const firstFrame = window.requestAnimationFrame(() => {
      const secondFrame = window.requestAnimationFrame(run);
      rafIds.push(secondFrame);
    });
    rafIds.push(firstFrame);
  }

  timeoutIds.push(window.setTimeout(run, 80));
  timeoutIds.push(window.setTimeout(run, 220));

  return () => {
    cancelled = true;
    timeoutIds.forEach((timeoutId) => window.clearTimeout(timeoutId));
    rafIds.forEach((rafId) => window.cancelAnimationFrame?.(rafId));
  };
}

export function AdminTenantsPage() {
  const navigate = useNavigate();
  const billingSectionRef = useRef<HTMLElement | null>(null);
  const billingResultsRef = useRef<HTMLDivElement | null>(null);
  const [searchParams] = useSearchParams();
  const selectedTenant = searchParams.get("tenant")?.trim() ?? "";
  const selectedMonth = searchParams.get("month")?.trim() || currentMonthValue();
  const [scrollNonce, setScrollNonce] = useState(0);
  const [tenantInput, setTenantInput] = useState(selectedTenant);
  const [monthInput, setMonthInput] = useState(selectedMonth);
  const loadTenants = useCallback(
    async () => {
      const [overview, apiKeys, usageOverview, userPoints] = await Promise.all([
        getOverview(),
        getAPIKeys(),
        getUsageOverview(),
        getUserPointsOverview(),
      ]);

      return {
        overview,
        apiKeys,
        usageOverview,
        userPoints,
        tenantSummaries: buildTenantSummaries(apiKeys.items),
      };
    },
    [],
  );
  const { data, loading, error } = useRemoteData(loadTenants);
  const loadTenantBilling = useCallback(
    () => (selectedTenant ? getTenantBilling(selectedTenant, selectedMonth) : Promise.resolve(null)),
    [selectedTenant, selectedMonth],
  );
  const { data: billing, loading: billingLoading, error: billingError } = useRemoteData(
    loadTenantBilling,
    [loadTenantBilling],
  );

  useEffect(() => {
    setTenantInput(selectedTenant);
    setMonthInput(selectedMonth);
  }, [selectedTenant, selectedMonth]);

  useEffect(() => {
    if (!selectedTenant) {
      return;
    }

    return scheduleScrollIntoView(billingSectionRef.current);
  }, [selectedTenant, scrollNonce]);

  useEffect(() => {
    if (!selectedTenant || billingLoading || !billing) {
      return;
    }

    return scheduleScrollIntoView(billingResultsRef.current);
  }, [selectedTenant, billingLoading, billing, scrollNonce]);

  function updateSearch(nextTenant: string, nextMonth: string) {
    const tenant = nextTenant.trim();
    const month = nextMonth.trim() || currentMonthValue();
    const next = new URLSearchParams();
    if (tenant) {
      next.set("tenant", tenant);
      next.set("month", month);
    } else if (nextMonth.trim()) {
      next.set("month", month);
    }

    const search = next.toString();
    navigate(search ? `/tenants?${search}` : "/tenants");
  }

  function handleBillingSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    updateSearch(tenantInput, monthInput);
  }

  function handleViewTenantBilling(tenant: string) {
    if (isHTMLElement(document.activeElement)) {
      document.activeElement.blur();
    }
    setScrollNonce((value) => value + 1);
    updateSearch(tenant, monthInput);
  }

  if (loading) {
    return <LoadingSection text="正在加载租户治理视图..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "租户治理视图加载失败。"} />;
  }

  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="已发放租户" value={String(data.tenantSummaries.length)} />
        <StatCard
          label="启用密钥"
          value={String(data.apiKeys.items.filter((item) => item.status === "启用").length)}
        />
        <StatCard label="总调用数" value={String(data.usageOverview.total_requests)} />
        <StatCard label="成功率" value={data.usageOverview.success_rate} />
        <StatCard label="总费用" value={formatValue(data.usageOverview.total_cost)} />
        <StatCard label="总积分" value={formatValue(data.usageOverview.total_points)} />
      </div>

      {data.userPoints.items.length > 0 ? (
        <section className="section-card">
          <div className="section-card__header">
            <div>
              <h2>用户积分消耗</h2>
              <p>
                积分 = Token 费用 / {data.userPoints.points_divisor || 10000} 微元，不同模型单价不同，贵的模型消耗更多积分。
              </p>
            </div>
            <p>共 {data.userPoints.items.length} 位用户</p>
          </div>
          <DataTable
            columns={["用户", "邮箱", "租户", "请求数", "Token / 费用", "积分"]}
            rows={data.userPoints.items.map((item) => [
              item.user_name || item.user_id,
              item.user_email || "-",
              item.tenant_name || item.tenant_id,
              String(item.request_count),
              `${item.total_tokens} / ${item.total_cost}`,
              item.total_points,
            ])}
          />
        </section>
      ) : null}

      <section className="section-card">
        <div className="section-card__header">
          <div>
            <h2>租户治理列表</h2>
            <p>基于当前密钥清单聚合出最小租户治理视图。</p>
          </div>
          <p>共 {data.tenantSummaries.length} 个租户</p>
        </div>
        <DataTable
          columns={["租户 ID", "密钥数", "启用中", "示例密钥", "权限范围", "操作"]}
          rows={data.tenantSummaries.map((item) => [
            item.tenant,
            String(item.keyCount),
            String(item.activeKeyCount),
            item.sampleKeyName,
            item.scopes || "-",
            <button
              key={`view-billing-${item.tenant}`}
              className="button-shell button-shell--table"
              onClick={() => handleViewTenantBilling(item.tenant)}
              type="button"
              aria-label={`查看账单 ${item.tenant}`}
            >
              查看账单
            </button>,
          ])}
        />
      </section>

      <div className="two-column-grid">
        <section className="section-card">
          <h3>平台调用概况</h3>
          <div className="detail-list">
            <div className="detail-list__row">
              <dt>总 Token</dt>
              <dd>{formatValue(data.usageOverview.total_tokens)}</dd>
            </div>
            <div className="detail-list__row">
              <dt>输入 Token / 费用</dt>
              <dd>{formatTokenCost(data.usageOverview.input_tokens, data.usageOverview.input_cost)}</dd>
            </div>
            <div className="detail-list__row">
              <dt>输出 Token / 费用</dt>
              <dd>{formatTokenCost(data.usageOverview.output_tokens, data.usageOverview.output_cost)}</dd>
            </div>
            <div className="detail-list__row">
              <dt>缓存 Token / 费用</dt>
              <dd>{formatTokenCost(data.usageOverview.cached_tokens, data.usageOverview.cached_cost)}</dd>
            </div>
            <div className="detail-list__row">
              <dt>总费用</dt>
              <dd>{formatValue(data.usageOverview.total_cost)}</dd>
            </div>
            <div className="detail-list__row">
              <dt>平均延迟</dt>
              <dd>{data.usageOverview.average_latency}</dd>
            </div>
            <div className="detail-list__row">
              <dt>估算占比</dt>
              <dd>{data.usageOverview.estimated_share}</dd>
            </div>
          </div>
        </section>

        <section className="section-card">
          <h3>平台总览摘录</h3>
          <div className="detail-list">
            <div className="detail-list__row">
              <dt>24 小时请求量</dt>
              <dd>{findOverviewStat(data.overview.stats, "24 小时请求量", "-")}</dd>
            </div>
            <div className="detail-list__row">
              <dt>配额使用率</dt>
              <dd>{findOverviewStat(data.overview.stats, "配额使用率", "-")}</dd>
            </div>
            <div className="detail-list__row">
              <dt>活跃 API 密钥</dt>
              <dd>{findOverviewStat(data.overview.stats, "活跃 API 密钥", "-")}</dd>
            </div>
          </div>
        </section>
      </div>

      {data.usageOverview.pricing_models.length > 0 ? (
        <section className="section-card">
          <div className="section-card__header">
            <div>
              <h3>模型定价口径</h3>
              <p>当前观测窗口内出现过的模型单价。</p>
            </div>
          </div>
          <DataTable
            columns={["模型", "输入单价", "输出单价", "缓存单价"]}
            rows={data.usageOverview.pricing_models.map((item) => [
              item.model,
              formatValue(item.input_price),
              formatValue(item.output_price),
              formatValue(item.cached_price),
            ])}
          />
        </section>
      ) : null}

      {data.overview.quota_summary?.configured ? (
        <section className="quota-summary-grid">
          <article className="quota-card">
            <span>平台月请求额度</span>
            <strong>
              {formatNumber(data.overview.quota_summary.requests_used)} /{" "}
              {formatNumber(data.overview.quota_summary.request_limit)}
            </strong>
            <p>
              剩余 {formatNumber(data.overview.quota_summary.requests_remaining)}
              {data.overview.quota_summary.resets_at
                ? `，重置时间 ${data.overview.quota_summary.resets_at}`
                : ""}
            </p>
          </article>
          <article className="quota-card">
            <span>平台月 Token 额度</span>
            <strong>
              {formatNumber(data.overview.quota_summary.tokens_used)} /{" "}
              {formatNumber(data.overview.quota_summary.token_limit)}
            </strong>
            <p>剩余 {formatNumber(data.overview.quota_summary.tokens_remaining)}</p>
          </article>
        </section>
      ) : null}

      <section ref={billingSectionRef} className="section-card">
        <div className="section-card__header">
          <div>
            <h2>租户账单详情</h2>
            <p>按自然月展示 summary、provider、model 与 API Key 账单分项。</p>
          </div>
        </div>
        <form className="filter-bar tenant-billing-filter" onSubmit={handleBillingSubmit}>
          <label className="field-shell">
            <span>租户 ID</span>
            <input
              aria-label="租户 ID"
              value={tenantInput}
              onChange={(event) => setTenantInput(event.target.value)}
            />
          </label>
          <label className="field-shell">
            <span>月份</span>
            <input
              aria-label="月份"
              type="month"
              value={monthInput}
              onChange={(event) => setMonthInput(event.target.value)}
            />
          </label>
          <button className="button-shell button-shell--primary" type="submit">
            应用筛选
          </button>
        </form>
        {!selectedTenant ? (
          <p>请在租户列表中点击“查看账单”以加载租户账单详情。</p>
        ) : null}
      </section>

      {selectedTenant ? (
        billingLoading ? (
          <LoadingSection text="正在加载租户账单..." />
        ) : billingError || !billing ? (
          <ErrorSection message={billingError ?? "租户账单加载失败。"} />
        ) : (
          <>
            <div ref={billingResultsRef} className="stats-grid stats-grid--four">
              <StatCard label="总请求" value={formatNumber(billing.summary.request_count)} />
              <StatCard
                label="成功 / 失败"
                value={`${formatNumber(billing.summary.success_count)} / ${formatNumber(billing.summary.failure_count)}`}
              />
              <StatCard label="总 Token" value={formatNumber(billing.summary.total_tokens)} />
              <StatCard label="总费用" value={billing.summary.total_cost || "-"} />
            </div>

            <section className="section-card">
              <div className="section-card__header">
                <div>
                  <h3>账单概览</h3>
                  <p>
                    {billing.summary.tenant_id} · {billing.summary.month}
                  </p>
                </div>
              </div>
              <DataTable
                columns={["指标", "数值"]}
                rows={[
                  [
                    "输入 Token / 费用",
                    `${formatNumber(billing.summary.input_tokens)} / ${billing.summary.input_cost}`,
                  ],
                  [
                    "输出 Token / 费用",
                    `${formatNumber(billing.summary.output_tokens)} / ${billing.summary.output_cost}`,
                  ],
                  [
                    "缓存 Token / 费用",
                    `${formatNumber(billing.summary.cached_tokens)} / ${billing.summary.cached_cost}`,
                  ],
                  [
                    "总 Token / 费用",
                    `${formatNumber(billing.summary.total_tokens)} / ${billing.summary.total_cost}`,
                  ],
                ]}
              />
            </section>

            <section className="section-card">
              <div className="section-card__header">
                <div>
                  <h3>Provider 分项</h3>
                  <p>基于 `llm_request_logs` 按 provider 聚合。</p>
                </div>
              </div>
              <DataTable
                columns={["显示名", "Provider", "请求", "成功", "失败", "Token", "费用"]}
                rows={billing.providers.map((item) => [
                  item.display_name,
                  item.provider,
                  formatNumber(item.request_count),
                  formatNumber(item.success_count),
                  formatNumber(item.failure_count),
                  formatNumber(item.total_tokens),
                  item.total_cost,
                ])}
              />
            </section>

            <section className="section-card">
              <div className="section-card__header">
                <div>
                  <h3>模型分项</h3>
                  <p>基于 `llm_request_logs` 按 model 聚合。</p>
                </div>
              </div>
              <DataTable
                columns={["模型", "Provider", "请求", "成功", "失败", "Token", "费用"]}
                rows={billing.models.map((item) => [
                  item.model,
                  item.provider_display_name,
                  formatNumber(item.request_count),
                  formatNumber(item.success_count),
                  formatNumber(item.failure_count),
                  formatNumber(item.total_tokens),
                  item.total_cost,
                ])}
              />
            </section>

            <section className="section-card">
              <div className="section-card__header">
                <div>
                  <h3>API Key 分项</h3>
                  <p>基于 `llm_request_logs` 按平台密钥聚合。</p>
                </div>
              </div>
              <DataTable
                columns={["名称", "Key ID", "请求", "成功", "失败", "Token", "费用"]}
                rows={billing.api_keys.map((item) => [
                  item.name,
                  item.platform_api_key_id,
                  formatNumber(item.request_count),
                  formatNumber(item.success_count),
                  formatNumber(item.failure_count),
                  formatNumber(item.total_tokens),
                  item.total_cost,
                ])}
              />
            </section>
          </>
        )
      ) : null}
    </div>
  );
}
