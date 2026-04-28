import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";

import { BrandMark } from "../components/brand-mark";
import { createApplication } from "../lib/console-api";

export function ApplicationFormPage() {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [companyName, setCompanyName] = useState("");
  const [useCase, setUseCase] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [submitted, setSubmitted] = useState<{
    name: string;
    status: string;
  } | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setError("");

    try {
      const result = await createApplication({
        email,
        name,
        company_name: companyName,
        use_case: useCase,
      });
      setSubmitted({
        name: result.item.name,
        status: result.item.status,
      });
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "申请提交失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="login-page">
      <div className="login-page__backdrop" aria-hidden="true" />
      <section className="login-page__hero">
        <BrandMark />
        <div className="login-page__headline">
          <p className="login-page__kicker">申请租户接入与平台 API Key</p>
          <h1>提交 AI Gateway 接入申请</h1>
          <p>提交后会进入 admin 审批队列。审批通过后，你会收到租户账号并可自助创建平台 API 密钥。</p>
        </div>
      </section>

      <section className="login-card">
        <div className="login-card__header">
          <span className="login-card__eyebrow">申请入口</span>
          <h2>申请接入</h2>
          <p>填写基础信息后提交，审批通过前不会分配可用密钥。</p>
        </div>
        <form className="login-form" onSubmit={handleSubmit}>
          <label className="field-shell">
            <span>邮箱</span>
            <input
              autoComplete="email"
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </label>
          <label className="field-shell">
            <span>姓名</span>
            <input value={name} onChange={(event) => setName(event.target.value)} />
          </label>
          <label className="field-shell">
            <span>公司</span>
            <input value={companyName} onChange={(event) => setCompanyName(event.target.value)} />
          </label>
          <label className="field-shell">
            <span>接入用途</span>
            <textarea rows={4} value={useCase} onChange={(event) => setUseCase(event.target.value)} />
          </label>
          {error ? <p className="login-form__error">{error}</p> : null}
          <button className="button-shell button-shell--primary login-form__submit" disabled={submitting} type="submit">
            {submitting ? "提交中..." : "提交申请"}
          </button>
        </form>

        {submitted ? (
          <div className="login-card__result">
            <h3>申请已提交</h3>
            <p>申请人：{submitted.name}</p>
            <p>状态：{submitted.status}</p>
          </div>
        ) : null}

        <div className="login-card__footer">
          <Link className="button-shell" to="/login">
            返回登录
          </Link>
        </div>
      </section>
    </div>
  );
}
