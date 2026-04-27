import { useCallback } from "react";

import { ErrorSection, LoadingSection } from "../components/console";
import { getMemberFailures } from "../lib/console-api";
import { useRemoteData } from "../lib/use-remote-data";

function toCount(value: string) {
  const matched = value.match(/\d+/);
  return matched ? Number(matched[0]) : 0;
}

export function MemberFailuresPage() {
  const loadFailures = useCallback(() => getMemberFailures(), []);
  const { data, loading, error } = useRemoteData(loadFailures);

  if (loading) {
    return <LoadingSection text="正在加载失败分析..." />;
  }

  if (error || !data) {
    return <ErrorSection message={error ?? "失败分析加载失败。"} />;
  }

  const strongestFailure = Math.max(1, ...data.breakdown.map((item) => toCount(item.value)));

  return (
    <div className="page-grid page-grid--usage">
      <div className="two-column-grid">
        <section className="section-card">
          <h2>失败分类</h2>
          <ul className="meter-list">
            {data.breakdown.map((item) => (
              <li key={item.label} className="meter-list__item">
                <div className="meter-list__meta">
                  <span>{item.label}</span>
                  <strong>{item.value}</strong>
                </div>
                <div className="meter-list__track">
                  <span
                    className="meter-list__fill"
                    style={{ width: `${(toCount(item.value) / strongestFailure) * 100}%` }}
                  />
                </div>
              </li>
            ))}
          </ul>
        </section>
        <section className="section-card">
          <h2>最近异常事件</h2>
          {data.recent_events.length > 0 ? (
            <ul className="event-timeline">
              {data.recent_events.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          ) : (
            <p>暂无异常事件</p>
          )}
        </section>
      </div>
    </div>
  );
}
