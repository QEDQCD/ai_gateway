export function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <section className="section-card">
      <p className="stat-card__label">{label}</p>
      <p className="stat-card__value">{value}</p>
    </section>
  );
}

function TableShell({
  title,
  columns,
  rows,
}: {
  title: string;
  columns: string[];
  rows: string[][];
}) {
  return (
    <section className="section-card">
      <h3>{title}</h3>
      <table className="data-table">
        <thead>
          <tr>
            {columns.map((column) => (
              <th key={column}>{column}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.join("-")}>
              {row.map((cell) => (
                <td key={cell}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

export function DashboardPage() {
  return (
    <div className="page-grid">
      <h2>Overview</h2>
      <div className="stats-grid">
        <StatCard label="Requests 24h" value="1.28M" />
        <StatCard label="Success Rate" value="99.42%" />
        <StatCard label="Quota Usage" value="74%" />
        <StatCard label="Active API Keys" value="184" />
      </div>
      <div className="two-column-grid">
        <TableShell
          title="Route Health"
          columns={["Requested Model", "Resolved Provider", "Latency", "Status"]}
          rows={[
            ["gpt-4o-mini", "OpenAI Primary", "218 ms", "Healthy"],
            ["text-embedding-3-small", "OpenAI Primary", "64 ms", "Healthy"],
            ["RAG Query", "RAG Service", "312 ms", "Warning"],
          ]}
        />
        <TableShell
          title="Top Models"
          columns={["Model", "Requests", "Share", "Mode"]}
          rows={[
            ["gpt-4o-mini", "612k", "48%", "Chat"],
            ["text-embedding-3-small", "301k", "24%", "Embedding"],
            ["RAG Query", "92k", "7%", "Knowledge"],
          ]}
        />
      </div>
      <div className="two-column-grid">
        <TableShell
          title="Recent Alerts"
          columns={["Time", "Type", "Scope"]}
          rows={[
            ["09:42", "Quota warning", "tenant_beta"],
            ["08:17", "Route fallback", "gpt-4o-mini"],
            ["07:03", "Latency spike", "rag-service"],
          ]}
        />
        <TableShell
          title="Audit Snapshot"
          columns={["Tenant", "Endpoint", "Status"]}
          rows={[
            ["tenant_alpha", "/v1/chat/completions", "200"],
            ["tenant_beta", "/v1/rag/query", "200"],
            ["tenant_gamma", "/v1/embeddings", "429"],
          ]}
        />
      </div>
    </div>
  );
}
