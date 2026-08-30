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
  {
    // 用 playwright 的验收脚本是**双环境**:顶层跑 Node,而 page.evaluate / addInitScript
    // 的回调体是序列化后放进浏览器执行的 —— 里面用的 window / document / location 是
    // 浏览器的。故两套全局都要给,否则这些浏览器侧代码会被误报 no-undef。
    // 上面那套的三个脚本(改名时记得同步这里的清单):
    //   - verify-map-vp-browser.mjs     视口尺寸测量时机
    //   - verify-map-browser.mjs        地图页最终渲染
    //   - verify-map-motion-browser.mjs 移动平滑度(RAF 逐帧)
    //   - verify-pip-canvas.mjs         画中画 canvas 像素
    //   - motion-report.mjs             偏差可视化报告
    //   - verify-admin-browser.mjs      管理面板失效令牌不黑屏
    //   - verify-admin-paging.mjs       游玩记录分页
    // 判定方法:顶层 import 'playwright' 的脚本就是双环境的,加进来即可。
    files: [
      'scripts/verify-map-vp-browser.mjs',
      'scripts/verify-map-browser.mjs',
      'scripts/verify-map-motion-browser.mjs',
      'scripts/verify-pip-canvas.mjs',
      'scripts/motion-report.mjs',
      'scripts/verify-admin-browser.mjs',
      'scripts/verify-admin-paging.mjs',
    ],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.node, ...globals.browser },
    },
  },
]
