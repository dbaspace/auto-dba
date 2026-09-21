/** @type {import('tailwindcss').Config} */
/** 对齐 universe-fe packages/metadata/tailwind.config.js */
import colors from 'tailwindcss/colors'

export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        gray: colors.slate,
        green: colors.emerald,
      },
      screens: {
        '3xl': '1920px',
      },
    },
  },
  darkMode: ['class', '[theme-mode="dark"]'],
  plugins: [],
  corePlugins: {
    // 与老项目一致：关闭 preflight，避免冲掉 AntD 全局样式
    preflight: false,
  },
}
