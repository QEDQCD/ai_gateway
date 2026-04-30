import { FormEvent, useState } from "react";
import { Link } from "react-router-dom";

import { BrandMark } from "../components/brand-mark";
import { loginConsole } from "../lib/console-api";
import { usePageMeta } from "../lib/page-meta";
import { saveConsoleSession } from "../lib/session";

export function LoginPage() {
  usePageMeta({
    title: "登录 AI Gateway 控制台",
    description: "登录 AI Gateway 控制台，统一管理平台 API Key、调用观测、审计与租户治理。",
    canonicalPath: "/login",
  });

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitting(true);
    setError("");

    try {
      const session = await loginConsole({
        email,
        password,
      });
      saveConsoleSession(session);
      window.location.assign("/");
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "登录失败，请稍后重试。");
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
          <p className="login-page__kicker">面向租户治理与密钥分发</p>
          <h1>登录 AI Gateway 控制台</h1>
          <p>
            统一管理平台 API Key、调用日志、失败记录与租户级 Token 消耗，
            对外隐藏上游模型细节，只暴露稳定的接入面。
          </p>
        </div>
        <div className="login-page__signal">
          <span>模型接入</span>
          <span>审计闭环</span>
          <span>租户配额</span>
        </div>
      </section>

      <section className="login-card">
        <div className="login-card__header">
          <span className="login-card__eyebrow">控制台入口</span>
          <h2>账号密码登录</h2>
          <p>使用管理员或普通用户账号进入对应控制台视图。</p>
        </div>
        <form className="login-form" onSubmit={handleSubmit}>
          <label className="field-shell">
            <span>账号</span>
            <input
              autoComplete="username"
              type="text"
              placeholder="请输入邮箱账号"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </label>
          <label className="field-shell">
            <span>密码</span>
            <input
              autoComplete="current-password"
              type="password"
              placeholder="请输入密码"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          {error ? <p className="login-form__error">{error}</p> : null}
          <button className="button-shell button-shell--primary login-form__submit" disabled={submitting} type="submit">
            {submitting ? "登录中..." : "进入控制台"}
          </button>
        </form>
        <div className="login-card__footer">
          <Link className="button-shell" to="/apply">
            申请账号
          </Link>
        </div>
      </section>
    </div>
  );
}
