import React, { useId } from 'react'
import './BrandMark.css'

/** 设计稿左侧盾牌 + 库 + 钥孔图标（viewBox 320²） */
export function BrandMarkIcon({
  size = 32,
  className = '',
}: {
  size?: number
  className?: string
}) {
  const uid = useId().replace(/:/g, '')
  const bg = `kp-${uid}-bg`
  const cy = `kp-${uid}-cy`
  const pu = `kp-${uid}-pu`

  return (
    <svg
      className={`brand-mark-icon ${className}`.trim()}
      width={size}
      height={size}
      viewBox="0 0 320 320"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-hidden
    >
      <defs>
        <linearGradient id={bg} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#1B4F8C" />
          <stop offset="1" stopColor="#3B8DFF" />
        </linearGradient>
        <linearGradient id={cy} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#00E5FF" />
          <stop offset="1" stopColor="#2F80FF" />
        </linearGradient>
        <linearGradient id={pu} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#8B5CF6" />
          <stop offset="1" stopColor="#FF4FD8" />
        </linearGradient>
      </defs>
      <rect width="320" height="320" rx="80" fill={`url(#${bg})`} />
      <ellipse
        cx="160"
        cy="160"
        rx="125"
        ry="46"
        fill="none"
        stroke="#8B5CF6"
        strokeWidth="5"
        opacity=".55"
        transform="rotate(-18 160 160)"
      />
      <ellipse
        cx="160"
        cy="160"
        rx="125"
        ry="46"
        fill="none"
        stroke="#00E5FF"
        strokeWidth="5"
        opacity=".55"
        transform="rotate(42 160 160)"
      />
      <path
        d="M160 50 L252 92 L252 182 C252 232 204 270 160 286 C116 270 68 232 68 182 L68 92 Z"
        fill="none"
        stroke={`url(#${cy})`}
        strokeWidth="10"
        strokeLinejoin="round"
      />
      <ellipse cx="160" cy="130" rx="46" ry="16" fill="#00E5FF" />
      <rect x="114" y="130" width="92" height="42" fill="#00E5FF" opacity=".5" />
      <ellipse cx="160" cy="172" rx="46" ry="16" fill="#00E5FF" />
      <circle cx="160" cy="160" r="24" fill="#fff" opacity=".96" />
      <circle cx="160" cy="154" r="10" fill="#1E3A5F" />
      <rect x="156" y="160" width="8" height="22" rx="4" fill="#1E3A5F" />
      <g fill={`url(#${pu})`}>
        <rect x="238" y="88" width="30" height="9" rx="4.5" />
        <rect x="248.5" y="77.5" width="9" height="30" rx="4.5" />
      </g>
      <circle cx="48" cy="220" r="10" fill="#00E5FF" />
      <circle cx="282" cy="128" r="10" fill="#8B5CF6" />
    </svg>
  )
}

/** 侧栏字标：展开 = 图标 + KeePlus；收起 = 仅图标；textOnly 仅字标（登录页旁已有大图标） */
export function BrandWordmark({
  collapsed = false,
  textOnly = false,
  className = '',
}: {
  collapsed?: boolean
  textOnly?: boolean
  className?: string
}) {
  if (textOnly) {
    return (
      <span className={`brand-wordmark ${className}`.trim()} aria-label="KeePlus" title="KeePlus">
        <span className="brand-kee">Kee</span>
        <span className="brand-plus">Plus</span>
      </span>
    )
  }
  if (collapsed) {
    return (
      <span className={`brand-lockup brand-lockup-compact ${className}`.trim()} aria-label="KeePlus" title="KeePlus">
        <BrandMarkIcon size={28} />
      </span>
    )
  }
  return (
    <span className={`brand-lockup ${className}`.trim()} aria-label="KeePlus" title="KeePlus">
      <BrandMarkIcon size={30} />
      <span className="brand-wordmark">
        <span className="brand-kee">Kee</span>
        <span className="brand-plus">Plus</span>
      </span>
    </span>
  )
}

/** 登录等场景的方形图标 */
export default function BrandMark({ size = 36 }: { size?: number }) {
  return (
    <span className="brand-mark" style={{ width: size, height: size }} aria-label="KeePlus" role="img">
      <BrandMarkIcon size={size} />
    </span>
  )
}
