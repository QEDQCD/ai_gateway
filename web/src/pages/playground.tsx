export function PlaygroundPage() {
  return (
    <div className="page-grid">
      <div className="two-column-grid">
        <section className="section-card">
          <h2>Playground</h2>
          <p>Model Selector: qwen-plus / text-embedding-v3 / rag-query</p>
          <p>Request Body: chat payload preview with tenant, model, and sampling controls.</p>
          <div className="page-actions">
            <button type="button" className="button-shell">
              Send Routed Request
            </button>
            <button type="button" className="button-shell">
              Reset Draft
            </button>
          </div>
        </section>
        <section className="section-card">
          <h3>Last Response</h3>
          <p>Resolved Provider: OpenAI Primary</p>
          <p>Endpoint: /v1/chat/completions</p>
          <p>Latency: 218 ms</p>
          <p>Status: 200 OK</p>
        </section>
      </div>
      <section className="section-card">
        <h3>Execution Meta</h3>
        <p>Platform Key: prod-gateway</p>
        <p>Resolved Provider: OpenAI Primary</p>
        <p>Endpoint: /v1/chat/completions</p>
      </section>
    </div>
  );
}
