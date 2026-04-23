export function AuditPage() {
  return (
    <div className="page-grid">
      <div className="page-actions">
        <button type="button" className="button-shell">
          Endpoint
        </button>
        <button type="button" className="button-shell">
          Status
        </button>
        <button type="button" className="button-shell">
          Tenant
        </button>
      </div>
      <section className="section-card">
        <h2>Audit</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Tenant</th>
              <th>Endpoint</th>
              <th>Status</th>
              <th>Provider</th>
              <th>Latency</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>09:42</td>
              <td>tenant_alpha</td>
              <td>/v1/chat/completions</td>
              <td>200</td>
              <td>OpenAI Primary</td>
              <td>218 ms</td>
            </tr>
            <tr>
              <td>09:39</td>
              <td>tenant_beta</td>
              <td>/v1/rag/query</td>
              <td>200</td>
              <td>RAG Service</td>
              <td>312 ms</td>
            </tr>
          </tbody>
        </table>
      </section>
      <div className="two-column-grid">
        <section className="section-card">
          <h3>Error Summary</h3>
          <p>Quota exceeded and routing fallback events surface here for operational review.</p>
        </section>
        <section className="section-card">
          <h3>Quota Exceeded</h3>
          <p>
            2 tenant throttles detected in the last hour, both isolated to batch embedding
            workloads.
          </p>
        </section>
      </div>
    </div>
  );
}
