export function APIKeysPage() {
  return (
    <div className="page-grid">
      <div className="page-actions">
        <button type="button" className="button-shell">
          Create Key
        </button>
        <button type="button" className="button-shell">
          Rotate Key
        </button>
        <button type="button" className="button-shell">
          Disable Key
        </button>
      </div>
      <section className="section-card">
        <h2>API Keys</h2>
        <table className="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Tenant</th>
              <th>Status</th>
              <th>Scope</th>
              <th>Last Used</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>prod-gateway</td>
              <td>tenant_alpha</td>
              <td>Active</td>
              <td>chat, rag</td>
              <td>2m ago</td>
            </tr>
            <tr>
              <td>batch-worker</td>
              <td>tenant_beta</td>
              <td>Active</td>
              <td>embeddings</td>
              <td>14m ago</td>
            </tr>
          </tbody>
        </table>
      </section>
      <section className="section-card">
        <h3>Credential Model</h3>
        <p>
          Platform API keys stay separate from provider credentials. BYOK is reserved for
          later phases.
        </p>
      </section>
    </div>
  );
}
