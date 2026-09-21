import React from 'react'

/** auto-dba 专属品牌标：A/D 咬合字母组合 + 自动化折线，避免通用数据库圆柱 */
export default function BrandMark({ size = 36 }: { size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 40 40"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="auto-dba"
    >
      <rect width="40" height="40" rx="10" fill="#EEF1F8" />
      {/* A */}
      <path
        d="M7 31 L15.5 8 H19 L14.2 22.5 H24.3 L22.6 27.5 H12.4 L10.2 31 H7Z"
        fill="#1F2937"
      />
      <path d="M13.6 19 H22.2 L21.2 22.2 H12.7 L13.6 19Z" fill="#4F5FB8" />
      {/* D */}
      <path
        d="M21 8 H26.5C32.3 8 36 12.2 36 20C36 27.8 32.3 32 26.5 32H21V8Z"
        fill="#4F5FB8"
      />
      <path
        d="M24.2 12.2V27.8H26.4C29.9 27.8 32.2 24.7 32.2 20C32.2 15.3 29.9 12.2 26.4 12.2H24.2Z"
        fill="#EEF1F8"
      />
      {/* 自动化轨迹 */}
      <path
        d="M22.8 16.5H26.2L27.8 14.2L30 20.2L31.8 17.4"
        stroke="#1F2937"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
