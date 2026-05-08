import type { ReactNode } from "react";

export function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <section className="section-card">
      <p className="stat-card__label">{label}</p>
      <p className="stat-card__value">{value}</p>
    </section>
  );
}

export function TableSection({
  title,
  columns,
  rows,
}: {
  title: string;
  columns: string[];
  rows: ReactNode[][];
}) {
  return (
    <section className="section-card">
      <h3>{title}</h3>
      <DataTable columns={columns} rows={rows} />
    </section>
  );
}

export function DataTable({
  columns,
  rows,
  emptyMessage = "暂无数据",
  onRowClick,
  rowClassName,
  tableClassName = "",
}: {
  columns: string[];
  rows: ReactNode[][];
  emptyMessage?: string;
  onRowClick?: (rowIndex: number) => void;
  rowClassName?: (rowIndex: number) => string;
  tableClassName?: string;
}) {
  return (
    <table className={`data-table ${tableClassName}`.trim()}>
      <thead>
        <tr>
          {columns.map((column) => (
            <th key={column}>{column}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.length > 0 ? (
          rows.map((row, rowIndex) => (
            <tr
              key={`row-${rowIndex}`}
              className={rowClassName?.(rowIndex)}
              onClick={onRowClick ? () => onRowClick(rowIndex) : undefined}
              onKeyDown={
                onRowClick
                  ? (event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        onRowClick(rowIndex);
                      }
                    }
                  : undefined
              }
              role={onRowClick ? "button" : undefined}
              tabIndex={onRowClick ? 0 : undefined}
            >
              {row.map((cell, cellIndex) => (
                <td key={`cell-${rowIndex}-${cellIndex}`}>{cell}</td>
              ))}
            </tr>
          ))
        ) : (
          <tr>
            <td colSpan={columns.length} className="table-empty-cell">
              {emptyMessage}
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

export function StatusPill({
  label,
  tone,
}: {
  label: string;
  tone: "success" | "warning" | "danger" | "neutral";
}) {
  return (
    <span className={`status-pill status-pill--${tone}`} aria-label={`状态 ${label}`}>
      {label}
    </span>
  );
}

export function SourcePill({ label }: { label: string }) {
  return (
    <span className="source-pill" aria-label={`来源 ${label}`}>
      {label}
    </span>
  );
}

export function SummarySection({
  title,
  items,
  emptyMessage = "暂无内容",
}: {
  title: string;
  items: string[];
  emptyMessage?: string;
}) {
  return (
    <section className="section-card">
      <h3>{title}</h3>
      {items.length > 0 ? (
        <ul className="summary-list">
          {items.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      ) : (
        <p>{emptyMessage}</p>
      )}
    </section>
  );
}

export function MetricSeriesSection({
  title,
  points,
  emptyMessage = "暂无趋势数据",
}: {
  title: string;
  points: Array<{ label: string; value: string }>;
  emptyMessage?: string;
}) {
  return (
    <section className="section-card">
      <h3>{title}</h3>
      {points.length > 0 ? (
        <ul className="metric-series-list">
          {points.map((point) => (
            <li key={`${point.label}-${point.value}`} className="metric-series-item">
              <span>{point.label}</span>
              <strong>{point.value}</strong>
            </li>
          ))}
        </ul>
      ) : (
        <p>{emptyMessage}</p>
      )}
    </section>
  );
}

export function LoadingSection({ text = "加载中..." }: { text?: string }) {
  return (
    <section className="section-card">
      <p>{text}</p>
    </section>
  );
}

export function ErrorSection({ message }: { message: string }) {
  return (
    <section className="section-card section-card--error">
      <p>{message}</p>
    </section>
  );
}

export function DetailList({
  items,
  emptyMessage = "暂无结果",
}: {
  items: Array<{ label: string; value: ReactNode }>;
  emptyMessage?: string;
}) {
  if (items.length === 0) {
    return <p>{emptyMessage}</p>;
  }

  return (
    <dl className="detail-list">
      {items.map((item) => (
        <div key={item.label} className="detail-list__row">
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}
