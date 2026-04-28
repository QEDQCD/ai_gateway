export function BrandMark({ compact = false }: { compact?: boolean }) {
  return (
    <div className={compact ? "brand-mark brand-mark--compact" : "brand-mark"}>
      <div className="brand-mark__icon" aria-hidden="true">
        <svg viewBox="0 0 96 96" role="img">
          <defs>
            <linearGradient id="brandBeam" x1="0%" y1="0%" x2="100%" y2="100%">
              <stop offset="0%" stopColor="#7df9d0" />
              <stop offset="100%" stopColor="#2f7bff" />
            </linearGradient>
          </defs>
          <rect x="10" y="10" width="76" height="76" rx="24" fill="#0e1726" />
          <path
            d="M27 59L46 27H59L40 59H27Z"
            fill="url(#brandBeam)"
            opacity="0.95"
          />
          <path
            d="M50 69L58 55H69L61 69H50Z"
            fill="#f6fbff"
            opacity="0.9"
          />
          <circle cx="67" cy="31" r="6" fill="#7df9d0" />
        </svg>
      </div>
      <div className="brand-mark__copy">
        <span className="brand-mark__eyebrow">AI Gateway</span>
        <strong className="brand-mark__title">{compact ? "控制台" : "统一接入控制台"}</strong>
      </div>
    </div>
  );
}
