import { StatCard } from "./dashboard";

export function RoutesPage() {
  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="Active Providers" value="4" />
        <StatCard label="Model Mappings" value="19" />
        <StatCard label="Fallback Policy" value="Enabled" />
        <StatCard label="Bootstrap Mode" value="Active" />
      </div>
      <section className="section-card">
        <h2>Routes</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Requested Model</th>
              <th>Resolved Provider</th>
              <th>Credential</th>
              <th>Latency</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>gpt-4o-mini</td>
              <td>OpenAI Primary</td>
              <td>provider_qwen_primary</td>
              <td>218 ms</td>
              <td>Healthy</td>
            </tr>
            <tr>
              <td>text-embedding-3-small</td>
              <td>OpenAI Primary</td>
              <td>provider_qwen_primary</td>
              <td>64 ms</td>
              <td>Healthy</td>
            </tr>
            <tr>
              <td>RAG Query</td>
              <td>RAG Service</td>
              <td>rag-service</td>
              <td>312 ms</td>
              <td>Warning</td>
            </tr>
          </tbody>
        </table>
      </section>
      <section className="section-card">
        <h3>Routing Policy</h3>
        <p>Bootstrap Mode: enabled</p>
        <p>Model-first Resolution: active</p>
        <p>
          Requests resolve to managed credentials before upstream dispatch, then fall back
          according to route policy.
        </p>
      </section>
    </div>
  );
}
