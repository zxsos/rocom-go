import js from '@eslint/js'
import globals from 'globals'
import react from 'eslint-plugin-react'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'

// 规则按「先能跑通、再逐步收紧」配置:
//   - no-unused-vars 只管没被引用的变量(函数声明提升常被误报,故只查变量与参数后的 args);
//   - react-hooks 两条规则是本次重构的主要守护(依赖缺失 / 条件调用 Hook);
//   - 其余主题(样式、目录)暂不引入额外插件,避免一次引入过多噪音。
export default [
  { ignores: ['dist', 'node_modules', '../internal/server/web'] },
  js.configs.recommended,
  {
    files: ['src/**/*.{js,jsx}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: globals.browser,
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
    },
    plugins: {
      react,
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // JSX 里引用的变量/React 本体,不开这两条 no-unused-vars 会满屏误报。
      'react/jsx-uses-react': 'error',
      'react/jsx-uses-vars': 'error',
      'react-refresh/only-export-components': 'off', // 页面文件里常把小组件与页面同文件导出
      'no-unused-vars': ['warn', { args: 'after-used', argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      'no-empty': ['error', { allowEmptyCatch: true }], // 空 catch 是本项目的既定写法(静默降级)
    },
  },
  {
    // 构建/验收脚本跑在 Node 里,不带浏览器全局
    files: ['scripts/**/*.mjs', 'vite.config.js', 'eslint.config.js'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: globals.node,
    },
  },
]
