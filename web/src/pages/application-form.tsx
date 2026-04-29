import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";

import { BrandMark } from "../components/brand-mark";
import { createApplication, issueCaptcha, verifyCaptcha } from "../lib/console-api";

export function ApplicationFormPage() {
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [companyName, setCompanyName] = useState("");
  const [useCase, setUseCase] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [captchaID, setCaptchaID] = useState("");
  const [captchaImage, setCaptchaImage] = useState("");
  const [captchaCode, setCaptchaCode] = useState("");
  const [captchaPassToken, setCaptchaPassToken] = useState("");
  const [captchaVerified, setCaptchaVerified] = useState(false);
  const [captchaLoading, setCaptchaLoading] = useState(false);
  const [captchaVerifying, setCaptchaVerifying] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [captchaMessage, setCaptchaMessage] = useState("");
  const [submitted, setSubmitted] = useState<{
    name: string;
    status: string;
  } | null>(null);

  useEffect(() => {
    void loadCaptcha();
  }, []);

  function validateForm() {
    if (email.trim() === "") {
      return "请输入邮箱。";
    }
    if (name.trim() === "") {
      return "请输入姓名。";
    }
    if (companyName.trim() === "") {
      return "请输入公司名称。";
    }
    if (useCase.trim() === "") {
      return "请输入接入用途。";
    }
    if (password.length < 8) {
      return "密码至少需要 8 位。";
    }
    if (password !== confirmPassword) {
      return "两次输入的密码不一致。";
    }
    if (!captchaVerified || captchaPassToken === "") {
      return "请先完成验证码验证。";
    }

    return "";
  }

  async function loadCaptcha() {
    setCaptchaLoading(true);
    setCaptchaMessage("");
    setCaptchaVerified(false);
    setCaptchaPassToken("");
    setCaptchaCode("");

    try {
      const challenge = await issueCaptcha();
      setCaptchaID(challenge.captcha_id);
      setCaptchaImage(challenge.image_data);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "验证码加载失败，请稍后重试。");
    } finally {
      setCaptchaLoading(false);
    }
  }

  async function handleVerifyCaptcha() {
    if (!captchaID || !captchaCode.trim()) {
      return;
    }

    setCaptchaVerifying(true);
    setError("");
    setCaptchaMessage("");

    try {
      const result = await verifyCaptcha({
        captcha_id: captchaID,
        captcha_code: captchaCode.trim(),
      });
      setCaptchaPassToken(result.captcha_pass_token);
      setCaptchaVerified(true);
      setCaptchaMessage("验证码已通过");
    } catch (nextError) {
      setCaptchaVerified(false);
      setCaptchaPassToken("");
      setCaptchaMessage("");
      setError(nextError instanceof Error ? nextError.message : "验证码校验失败，请稍后重试。");
    } finally {
      setCaptchaVerifying(false);
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");

    const validationError = validateForm();
    if (validationError) {
      setError(validationError);
      return;
    }

    setSubmitting(true);

    try {
      const result = await createApplication({
        email,
        name,
        company_name: companyName,
        use_case: useCase,
        password,
        captcha_pass_token: captchaPassToken,
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

  const canSubmit = !submitting;

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
          <label className="field-shell">
            <span>密码</span>
            <input
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          <label className="field-shell">
            <span>确认密码</span>
            <input
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(event) => setConfirmPassword(event.target.value)}
            />
          </label>
          <label className="field-shell">
            <span>验证码</span>
            <input value={captchaCode} onChange={(event) => setCaptchaCode(event.target.value)} />
          </label>
          <div className="field-shell">
            <span>图形验证码</span>
            {captchaImage ? <img alt="图形验证码" src={captchaImage} /> : <p>正在加载验证码...</p>}
            <div className="page-actions">
              <button type="button" className="button-shell" disabled={captchaLoading} onClick={() => void loadCaptcha()}>
                刷新验证码
              </button>
              <button
                type="button"
                className="button-shell"
                disabled={captchaVerifying || captchaLoading || !captchaID || !captchaCode.trim()}
                onClick={() => void handleVerifyCaptcha()}
              >
                验证验证码
              </button>
            </div>
            {captchaMessage ? <p>{captchaMessage}</p> : null}
          </div>
          {error ? <p className="login-form__error">{error}</p> : null}
          <button className="button-shell button-shell--primary login-form__submit" disabled={!canSubmit} type="submit">
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
