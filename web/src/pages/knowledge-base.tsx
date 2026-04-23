import { StatCard } from "./dashboard";

export function KnowledgeBasePage() {
  return (
    <div className="page-grid">
      <div className="stats-grid">
        <StatCard label="Documents" value="184" />
        <StatCard label="Chunks" value="12.4k" />
        <StatCard label="Last Ingest" value="8m ago" />
        <StatCard label="Queue Status" value="Healthy" />
      </div>
      <section className="section-card">
        <h2>Knowledge Base</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Knowledge Base</th>
              <th>Documents</th>
              <th>Status</th>
              <th>Updated At</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>Product Docs</td>
              <td>84</td>
              <td>Ready</td>
              <td>09:10</td>
            </tr>
            <tr>
              <td>Support Archive</td>
              <td>62</td>
              <td>Indexing</td>
              <td>08:44</td>
            </tr>
          </tbody>
        </table>
      </section>
      <div className="two-column-grid">
        <section className="section-card">
          <h3>RAG Query Flow</h3>
          <p>
            Query enters the gateway, resolves to the RAG service, then joins retrieval
            context before final response assembly.
          </p>
        </section>
        <section className="section-card">
          <h3>Ingest Queue</h3>
          <p>3 files pending chunk refresh, 1 index rebuild in progress, no failed ingest jobs.</p>
        </section>
      </div>
    </div>
  );
}
