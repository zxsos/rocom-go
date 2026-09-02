# 妙妙屋 · UI 美化升级计划

> 接续 P1（系别色对比度 AA / 配色收进变量 / 二级菜单键盘操作）与 P2（阅读栏宽 /
> hover 与入场反馈 / 数字显示字体）之后的分阶段美化计划。本文件由前端区域维护，
> 不占 `docs/` 公共区。每阶段验收都要跑 `npm run build` 并肉眼过一遍深/亮两主题。

## 0. 现状基线（已完成，勿重复做）

| 方面 | 已有能力 |
| --- | --- |
| 主题 | 深/亮/auto 三态，完整设计令牌变量（颜色/阴影/圆角/间距/语义色） |
| 可访问性 | 系别色对比度 AA、全局焦点环、二级菜单键盘操作、`prefers-reduced-motion` |
| 排版 | 阅读栏宽 1720px、自托管数字字体 Bricolage Grotesque(13KB)、中文标题字体思源黑体 Bold 子集(238KB, `--font-display`) |
| 交互 | hover/入场反馈、自研 Toast/Confirm/Dropdown/PinDialog、`TweenNumber` 数字滚动 |
| 数据层 | `useAsyncData` 竞态守卫（错误保留旧数据）、`useAsyncRun` |
| 图片 | 全部 `loading="lazy"` + `onError` 兜底；后端 `/img/` `Cache-Control: max-age=86400` |
| 移动端 | 底部 tab、触控基线、`useScrollLock`/`useWakeLock` |
| 隐私 | 截图防泄遮罩（brand 即开关） |

**当前主要缺口**：单 bundle 无路由分包、页面切换无过渡、loading 无骨架屏、
中文标题无品牌字体、平板断点未审计、弹窗焦点管理未闭环、图片管线未压缩到最优。

---

## 1. 优先级总览

| 优先级 | 阶段 | 主题 | 理由 |
| --- | --- | --- | --- |
| **P0** | P3 | 性能地基（分包/缓存） | 改动集中、风险低、首屏体感收益最大，且为后续动效腾出帧预算 |
| **P0** | P4 | 视觉语言（字体/按钮/排版） | 品牌感核心，纯静态改动，安全 |
| **P1** | P5 | 交互体验（动效/过渡/骨架屏） | 紧接视觉，UI 打磨，需以 P3 的懒加载 Suspense 为骨架 |
| **P1** | P6 | 可访问性审计 | 改动分散但边界清晰，合规兜底 |
| **P2** | P7 | 图片管线 | 动生成脚本 + 重跑 unpack，成本高、与版本更新流程耦合 |
| **P2** | P8 | 响应式/布局补强 | 范围大、边际收益，最后收尾 |

---

## 2. P3 · 性能地基（优先级 P0，先做）—— ✅ 已完成

> 实施记录：路由懒加载(11 页 lazy + Suspense)、`manualChunks` 拆 vendor-react、
> `handleStatic` 三档缓存头(`assets/` immutable 1 年 / `fonts/` 1 天 / 其余 no-cache，
> 含 `internal/server/cache_headers_test.go` 7 例单测)、首屏 JS 395KB→296KB(-25%)、
> 列表 `content-visibility`。地图推流核实为 rAF 驱动+静止停帧，无需改动。

### 2.1 路由级代码分割（收益最大）

现状：`web/src/main.jsx` 静态 import 全部 11 个页面，地图引擎（`useMapEngine`/
`pipDraw`/`pipGeom`）也打进同一 bundle。局域网工具站首屏要的是"秒开"，地图页不该
拖累宠物列表。

步骤：
1. `main.jsx` 中除 `App`、`PetList`（默认落地页）外的页面全部改 `React.lazy`；
2. 包一层 `<Suspense fallback={...}>`，fallback 复用 P5 的骨架屏（可先给简单 spinner）；
3. `vite.config.js` 加 `build.rollupOptions.output.manualChunks`：把 `map` 页依赖
   （canvas/引擎相关）单独拆 chunk，避免地图代码进入首屏；
4. 验收：`npm run build` 后看 `internal/server/web/assets/` 分包数量与大小；
   首屏 JS 体积目标下降 ≥40%。

预期效果：首屏 JS 体积显著下降（地图引擎不再进首屏），局域网内首屏渲染时间缩短，
弱设备上滚动/交互掉帧减少。

### 2.2 构建产物压缩与资源提示

1. `vite.config.js` 开启 `build.minify`（默认 esbuild，确认即可）+ `cssCodeSplit`；
2. 首页 `<link rel="preload">` 数字字体与 `logo.svg`；对首屏关键图（宠物头像）
   加 `fetchpriority="high"`（P7 图片管线的 DOM 侧配合）；
3. 确认 `handleStatic` 对 `web/` 产物（带 hash 文件名）返回长缓存
   （`public, max-age=31536000, immutable`），`index.html` 保持 `no-cache`。
   当前仅 `/img/` 有缓存头，`handleStatic` 需补 —— 改动在 `internal/server/`，
   **属后端区域，动手前先与后端 owner 打招呼**（见 AGENTS.md 跨区约束）。

预期效果：后续发布更新时浏览器只增量拉 hash 变化的文件；字体/首屏资源提前就位。

### 2.3 渲染性能体检

1. 宠物列表滚动用 `content-visibility: auto` 给列表项加渲染跳过（先测兼容性）；
2. 检查 `useMapEngine` 的推流节流是否复用 `requestAnimationFrame`，避免每帧 setState。

预期效果：长列表滚动与地图推流在低端机上的帧率提升，交互保持 60fps。

---

## 3. P4 · 视觉语言（优先级 P0）

### 3.1 中文标题字体（品牌感核心）—— ✅ 已完成

现状：中文仍用系统字体栈（`-apple-system`/`PingFang SC`），标题与正文无层级区分。

实施记录：
- 选型：**思源黑体 / Noto Sans CJK SC Bold(700)**（SIL OFL 1.1，自托管零依赖），
  入库 `web/public/fonts/source-han-sans-sc-700-subset.woff2` + `OFL-NotoSansCJK.txt`；
- 子集规模：标题字(20) + **宠物名(830)** + ASCII/标点 = **945 字 / 238KB**。
  **预算偏差说明**：计划原估 ≤150KB(仅 300–500 标题字)。实际把详情弹窗 h2 的宠物名
  (20485 个宠物名，唯一字符 830 个)一并收录，因为宠物名是全站出现最多的大字——
  不收会让大量标题逐字回退系统字体、跨平台(win 雅黑/mac 苹方)不一致。238KB 在局域网
  内加载可忽略(约 20ms@100Mbps)，换来的是**零回退**；
- 令牌：`:root` 新增 `--font-display`（回退链以系统字体收尾）；`@font-face` 注册
  `RocoDisplay`(700, swap, unicode-range 限定子集范围，子集外逐字回退不出豆腐块)；
- 应用范围：全局 `h1,h2` + 顶栏站名 `.brand`。**h3 及正文保持系统字体**——显示字体
  只有 700 一档，字号小的 h3 用它会过重；数字仍走 Bricolage(`--font-num`)；
- 覆盖校验：`fontTools` 验证全部 h2/站名 + 全部 20485 个宠物名 **0 缺失**；
- 复现：新增标题/宠物名文字后跑 `node web/scripts/subset-display-font.mjs`
  (自动收集字符集 + `uv run pyftsubset`，源字体 17MB 需 `--download` 或 `--font` 指定)。

预期效果：标题与正文形成字体层级，站名/页码/宠物名的辨识度与品牌感提升，且
mac/win/linux/安卓多端渲染一致。

### 3.2 排版标度统一 —— ✅ 已完成（间距标度按"低收益"归档）

实施记录：
1. **标题字号标度**：`:root` 新增 `--text-h1/h2/h3`（26/20/15px），全局规则
   `h1 { --text-h1 }`、`h2 { --text-h2 }`、`h3 { --text-h3 }`。
   **审计发现**：页面级 h2 大多没设字号 → 吃浏览器默认 24px，与 admin 20px、
   handbook 17px 三者不一致；统一后页面级标题一律 20px。
   消掉的魔数：`.hb-title` 17px、`.admin-head h2` 20px（继承全局）。
   刻意保留的例外（注释登记在 base.css）：admin-card h2 18px（管理卡片密集）、
   移动端 admin-head h2 18px。
   **正文 11-13px 不纳入标度**：数据密集节奏，token 化纯 churn 无视觉收益。
2. **间距 `--space-*` 标度**：**未做，归档**。全站间距值分布广、每处语义独立
   （卡片距/分组距/弹窗内边距互不通用），一次性迁移是纯 churn；现有间距已自洽，
   无可见问题。留给将来真正遇到"间距改一处要动十处"时再立标度。
3. **阅读栏宽**：核实 `1720px` 已实现且带完整注释（`margin-inline: auto` 居中 +
   超宽屏上限说明），符合 `--reading-max` 目标，无需改动。

预期效果：页面节奏一致、层级清晰，深/亮主题下间距语义不变形。

### 3.3 按钮设计体系（Button System）—— ✅ 已完成（剩 2 项收尾见下）

现状：`.btn` 已有 hover 边框变色/背景、`:active` 回弹、`:focus-visible` 焦点环、
`disabled`，`.icon-btn`/`.chip`/`.pill` 各有一套。缺口：**无变体矩阵、无尺寸档位、
无 loading 态、可点元素视觉语言不统一**（部分地方仍用裸 `<button>` 或纯文字冒充可点）。

实施记录：
1. **变体矩阵**（已收进 `base.css`，全部走语义色）：
   - `default`/`primary`：沿用现有，`primary:hover` 硬编码 `#5c9aff` 收进新令牌
     `--accent-hi`（明暗主题各一档，口径同 `--red-hi`）；
   - `danger`（新增）：`--red` 底白字，hover 用 `--red-hi`；
   - `ghost`（统一）：原分散在 admin.css/merchant.css 的两份 `.btn.ghost`/
     `.btn-ghost` 定义删掉，收进 base —— 透明底 + 边框 + 灰字，hover 给主色
     （**修语义 bug**：原 admin 版 ghost hover 一律变危险红，连"退订/取消"也红）；
   - `ghost.danger`（新增，弱化危险）：红字不铺红底，列表里的"删除/撤销/清 PIN/
     删除账号/删除该槽"统一换用，降低误触压迫感；
   - 注释写明语义分级（主操作 ≤1 个 primary）。
2. **尺寸档位**：`.btn.small` 统一进 base（删 events.css 重复定义，移动端
   `min-height: 30px` 触控基线保留）；**未加 `.large`**（无真实使用场景，不造死类）；
   图标按钮（`.btn-icon`/`.icon-btn`）已是独立标度，不并入。
3. **loading 态**：`.btn.is-loading` 内置 `currentColor` spinner（复用 `@keyframes
   spin`，任意变体自动配色）+ 隐藏文字 + 禁点 + reduced-motion 降速 1.6s；
   异步提交实测接入 2 处：邮件测试、强制刷新商人数据（原"发送中…"文本切换改 spinner）。
   **收尾项**：管理员登录、PIN 提交等其余异步按钮按需接入。
4. **icon + 文字**：`gap` 已统一；**hover 图标 +2px 位移未做**——需先有可寻址的
   图标元素（`.btn-ico` 约定），留到按钮组件化时一并做。
5. **可点性审计**：完成危险语义扫描（9 处破坏性操作全部换上 `ghost danger`/`danger`）；
   confirm 弹窗确定钮由 `confirm-danger` 类改走 `btn danger`（删死规则）；
   `btn-ghost` 统一命名 `btn ghost`。**收尾项**：`grep -rn "<button"` 逐处核对裸
   按钮与 `div`/`span` 冒充可点（可访问性同步受益）。

预期效果：全站按钮视觉/行为一致，破坏性操作一眼可辨，异步操作有明确的
"进行中"反馈，不再有"看着像按钮但点不了"或"能点但不像是按钮"的元素。

### 3.4 动画设计体系（Motion System）—— ✅ 已完成（分级中"通知/数据"待 P5/P6）

现状：时长/缓动**散落硬编码**（`.12s`/`.15s`/`.16s`/`.2s` 各处手写），
已有零散动效（hover 三件套、stagger 入场、toast/confirm、账号下拉、数字滚动、
地图平滑），但**没有统一的令牌与分级**。

实施记录：
1. **动效令牌**（已收进 `base.css` `:root`，注释含分级说明）：
   - `--dur-fast: 120ms`（hover/active 反馈，原 150ms 收紧更跟手）；
   - `--dur-base: 200ms`（toast/确认框/下拉显隐，原 160ms 统一）；
   - `--dur-slow: 320ms`（抽屉滑入，panel.css 移动端 filters 已接入，原 250ms 放宽）；
   - `--ease-out: cubic-bezier(.32, .72, 0, 1)`（系统抽屉曲线）；
   - **`--ease-spring` 未预置**：注释写明"回弹只留给庆祝类动效，当前无此场景，
     不造死令牌"（check-css-vars 会拒收未使用变量）。
   - **迁移范围**：base.css 全量（`.btn`/`.icon-btn`/`.chip`/`.input` 反馈、
     toast/confirm 显隐、glass-zoom 入场）+ panel.css 抽屉；shell.css 及各页面
     CSS 的时长仍为字面量（内部已自洽），留增量迁移。
2. **动效分级**（注释已登记在 `:root` 令牌处）：
   - 入场：弹窗 scale-in（confirm，已走 --dur-base）；列表 stagger（已有，保持）；
     页面路由过渡待 P5.1；
   - 反馈：hover/active 统一走 `--dur-fast`（`.btn`/`.icon-btn`/`.chip` 已迁移）；
   - 通知：toast 已走 --dur-base；"新捕获事件高亮 2s 后淡出"待 P6；
   - 数据：`TweenNumber` 400ms 保持；图表增长动画待 P6；
   - 禁止用动效的位置已写明（表格行/纯展示卡片不抬升）。
3. **`prefers-reduced-motion` 全覆盖**：新增浮层/抽屉统一关位移保留不透明度
   （toast/confirm/glass-zoom/抽屉 `transition-duration: 0.01ms !important`）；
   加号：loading spinner 降速 1.6s（P3 已有，P4 补 `.btn.is-loading` 同款）。
4. **性能约束**：全部新动效只用 `transform`/`opacity`（令牌迁移未新增动画
   属性）；stagger 上限 8 项保持；大列表 `content-visibility` 已落地（P3）。

预期效果：全站动效时长/节奏统一，快慢有据（反馈快、入场缓）；新增动效不再
"随手写个 transition"，而是按分级表选择；reduced-motion 用户与低端机体验不劣化。

---

## 4. P5 · 交互体验（优先级 P1）

### 4.1 页面切换过渡 —— ✅ 已完成

步骤（原计划）：
1. 路由出口包 `CSSTransition` 或轻量自研（`@keyframes` + `key` 触发）：页面淡入
   + 8px 上移，时长 160–200ms，`prefers-reduced-motion` 下直接无动画；
2. 复用现有入场反馈模式（P2 已做 hover/入场），保持"入场即反馈"的语言一致；
3. 不引入重量级动画库，保持 bundle 可控。

实施记录：
- 自研（不引库）：`App.jsx` 新增 `RouteEnter`，用 `key={pathname}` 换 key 触发
  CSS `animation`。**只包 `<Outlet/>` 不包外壳** —— 顶栏/底导航是固定 chrome，
  跟着内容一起动会显得整页在晃。
- 动画 `.route-enter`（base.css）：`opacity 0→1 + translateY(8px)→0`，
  `--dur-base` / `--ease-out`；reduced-motion 下 `animation: none`。
  **keyframes 不写 `to`、不设 fill-mode**：动画结束即回常态 `transform: none`，
  不留持久 containing block（地图页的定位与测量建立在「祖先无 transform」上）。
- **踩坑（已修）**：这一层是布局敏感的。map.css 的高度链是
  `.content:has(.map-page)` 变 flex 列 + `.map-page{flex:1 1 auto}` 撑满；
  插进一个普通块级 `.route-enter` 后，flex 落在非 flex 容器里失效 ——
  **实测 .map-page 高度归零（地图只剩顶部一条）**。修法：只在地图页让包装层
  接住并传递这条链（`.content:has(.map-page) > .route-enter{display:flex;
  flex-direction:column;flex:1 1 auto;min-height:0}`），他页保持普通块级。
- 新增 `web/scripts/verify-route-enter.mjs`（真浏览器 + 自带静态服务，**不依赖后端**，
  已并进 `npm run verify:browser`）：把 `.route-enter > .map-page` 探针注入真实
  `.content` 量高度。jsdom 版测不出（不跑真实布局）。
  **变异测试**：删掉上面那条修复规则 → `map-page 0.0px / 可用 709.0px`，2 项 FAIL；
  恢复 → 709.0px 对齐，3 项 ok。
- 测法本身也踩过一次坑：探针直接塞进已有真实内容的 `.content`，自由空间被真内容
  占满、量出来恒为 0（假阳性）。故探针前先隐藏既有子节点。

预期效果：页面切换不再"硬切"，视觉连贯、减少跳变突兀感。

### 4.2 骨架屏与加载反馈 —— ✅ 已完成

步骤（原计划）：
1. 新建 `Skeleton` 组件（CSS 渐变扫光，`aria-hidden`）；
2. 各列表页（宠物/蛋/商人/花）loading 时显示与真实布局同构的骨架，
   替代现有"加载中…"文字；
3. `useAsyncData` 已保证错误不清数据，配合骨架屏后首次加载体验完整闭环；
4. 图片 loading 占位：`avatar`/`icons` 组件在 `onLoad` 前显示统一灰底占位
   （含主题变量，避免深/亮两主题下闪白）。

实施记录：
- 新增 `components/Skeleton.jsx`：`Skeleton` / `SkeletonRows` / `SkeletonGrid`，
  色块一律 `aria-hidden`（外层容器自己补 `role="status"`）。
- 扫光只走主题令牌：`--bg-3` 底 + `--line` 高光（深色下 `--line` 更亮、浅色下更暗，
  两主题都是「微妙提亮」）。**不写死 `rgba(255,255,255,.x)`** —— 那是闪白的根源
  （浅色主题下等于白高光叠白底）。只动画 `background-position`，合成器友好。
- 落地 5 处：
  - 商人页（横幅 + 轮次卡纵向堆叠）、炫彩图鉴（**直接复用 `.hb-major-group` /
    `.hb-major-cards` 真实布局类**铺 42×23 的色卡格，结构与内容同构）、
    详情弹窗（复用 `.detail-card`/`.detail-body`/`.kv`）；
  - 宠物列表、排行榜：修的是**语义错报** —— 首次加载时 `data` 还是 fallback，
    原先直接渲染「没有匹配的宠物 / 暂无参加者」，那是**结论**不是「加载中」，
    会让人以为筛了个寂寞或榜单空着。现在首次加载铺骨架（换筛选条件时
    `useAsyncData` 保留旧数据，故只有真正从零开始那一次会见到骨架）。
- 图片占位（`icons.jsx` 新增 `useImgReady`）：占位底色挂在 `<img>` **自身**
  （`.img-ph`）而非另插元素 —— 图片尺寸本来就由各自的类定好，没有多余 DOM，
  也不会出现「占位与图片并存」导致的跳位。
  **关键坑**：必须在 ref 回调里补查 `el.complete` —— 命中缓存时浏览器可能在
  React 挂上 `onLoad` 之前就完成加载，那时 `onLoad` 永不触发，只靠 `onLoad`
  会让缓存命中的图永远停在占位态（刷新一次才正常，极易被误判成偶发 bug）。
- 未做（有理由，非遗漏）：
  - 花种页 —— 加载由下拉 `placeholder="加载中…"` 表达，且槽位切换有既有语义
    （`slots` 未到时回落到当前世界），改骨架要动既有行为；
  - 精灵蛋页 —— 数据是 SSE 推送驱动，空态给的是「需打开一次背包」这种
    **明确原因说明**，不是等待态，铺骨架反而丢信息。

预期效果：数据等待期有结构感而非空白/文字，首屏与刷新体验提升。

### 4.3 弹窗焦点管理 —— ✅ 已完成

步骤（原计划）：
1. `PinDialog`/`PetDetailModal` 打开时焦点移入弹窗、`Tab` 循环圈定在弹窗内、
   关闭后焦点还给触发按钮（可封装 `useDialog` hook，复用 `useScrollLock` 的模式）；
2. 遮罩点击/`Esc` 关闭已有的话保持，补 `role="dialog"`/`aria-modal`。

实施记录：
- 新增 `hooks/useDialog.js`（与 `useScrollLock` 同一套路：开→生效，关→还原），
  返回 `{ ref, dialogProps }`（`dialogProps` = `role="dialog"` + `aria-modal="true"`
  + `tabIndex={-1}` + `aria-label`，都挂弹窗最外层容器）。
  **不接管 Esc/遮罩关闭**（各弹窗已有，避免重复绑定）。
- 三件事：打开时聚焦弹窗内第一个可聚焦元素（组件已自行聚焦时**不抢**）；
  Tab / Shift+Tab 首尾循环；关闭后把焦点还给打开它的元素（该元素可能已随数据
  消失，此时 `focus()` 空操作、落到 body —— 也比停在已移除的节点上强）。
- 两个实现细节（都是坑）：
  - 可见性用 `getClientRects().length > 0` 判，**不用 `offsetParent !== null`** ——
    后者对 `position: fixed` 恒为 null，而弹窗容器本身就是 fixed；
  - 监听挂 `document`（捕获阶段）而非容器：焦点一旦意外跑出容器（地址栏绕回、
    外部脚本聚焦），挂在容器上就再也拦不住了；且焦点不在弹窗内时一律拉回。
- 接入：`PinDialog`（顺带删掉三处 `inputRef` 手动聚焦 —— hook 自动聚焦的第一个
  可聚焦元素在各模式下恰好就是「该填的那个 PIN 框」，行为一致且少一套 ref 管道）、
  `PetDetailModal`（加载态与内容态共用同一个 ref，DOM 节点复用不重挂）。
- 遗留：未做浏览器端的自动化验收（焦点行为需真实交互，`verify-account-mobile.mjs`
  那套键盘测试可作参照）。人工确认路径：Tab 走不到弹窗背后、关闭后焦点回到原卡片。

预期效果：键盘用户操作弹窗不"迷失焦点"，与 P1 的二级菜单键盘操作形成完整链路。

### 4.4 主题切换：从按钮处圆形扩散 —— ✅ 已完成

需求：点顶栏主题按钮换深/亮时，新主题**从按钮那里慢慢蔓延开**，而不是整屏瞬变。

实施记录：
- 用 **View Transitions API**（不引库）：`document.startViewTransition()` 截下「旧主题」
  与「新主题」两张整页快照，让**新**快照以按钮中心为圆心做
  `clip-path: circle(0 → 覆盖整屏)` 展开 —— 新色从按钮里长出来，把旧色推到屏幕外。
  时长新增令牌 `--dur-theme: 620ms`（比 `--dur-slow` 再长一档：它是唯一的全屏动效，
  320ms 会显得「唰」一下盖过去）。
- 圆心/半径由 `hooks/useTheme.js` 在点击时**内联注入** `--theme-x/y/r`：
  - 圆心取 `e.currentTarget.getBoundingClientRect()` 的中心 —— 故 `App.jsx` 必须写
    `onClick={cycleTheme}`（事件即圆心来源），包一层 `() => cycleTheme()` 就丢了事件；
  - 半径取到视口**四角**的最远距离（`Math.hypot(max(x, w-x), max(y, h-y))`），
    短一点会在对角留一条月牙形旧色残边。
- 三个坑：
  1. **必须 `flushSync(() => setTheme(next))`**：回调返回后浏览器立刻截新快照，
     而 React 18 的自动批处理会把更新推到微任务 —— 快照可能截到旧主题，
     表现为「圆张开了，里面铺开的还是原来那个颜色」。另在回调末尾按新模式再落一次
     `data-theme`（幂等），不依赖 effect 的调度时序。
  2. **必须关掉 UA 默认的交叉淡入**（`::view-transition-old/new(root)` 写
     `animation: none; mix-blend-mode: normal`）：否则圆还没张到的地方也跟着整体变淡，
     观感是「先整屏闪一层灰再扩散」；`mix-blend-mode` 不改则是 plus-lighter 越叠越亮。
  3. `theme-spread` class 与三个变量的清理要同时挂 `vt.ready.catch()` 与
     `vt.finished.then()` —— 连续快点时第一次过渡会被抢占，只有 `finished` 会漏掉残留。
- 三条退化路径（动画可以没有，功能不能丢）：无 `startViewTransition`、
  `prefers-reduced-motion: reduce`、**换了模式但生效颜色没变**（浅色浏览器里
  auto → light）—— 最后那条尤其要不得：全程看不到任何变化，而过渡期间页面是快照
  （连按钮图标都要等过渡结束才换），620ms 的观感就是「点了没反应」，故直接瞬时切。
- 验收（两套，谁也替不了谁）：
  - `scripts/verify-theme-spread.mjs`（静态，已并进 `npm run verify`）：守住
    「关默认淡入」「裁的是 new 而不是 old」「半径覆盖最远角」「用了 flushSync」
    「reduced-motion 有降级」等 8 类静默失效。**变异测试 8/8 全被抓到**。
  - `scripts/verify-theme-spread-browser.mjs`（真 Chromium，不依赖后端）：
    断言动画真的挂在 `::view-transition-new(root)` 上（选择器匹配得上）、
    中途 clip-path 半径介于 0 与满半径之间（真的在逐帧长大）、收尾清理干净，
    外加三条退化路径与一条**像素证据**（中途截图取色：圆内 A 点已是新主题、
    圆外 B 点仍是旧主题）。
  - 变异测试（5 条，记录真实的覆盖率而不是好看的数字）：
    - 选择器写错 / 不关默认淡入 / 去掉收尾清理 / 去掉「颜色不变就不播」→ 4 条全被抓到；
    - **「去掉 flushSync」当前抓不到** —— Chromium 上 React 赶得及在截快照前提交，
      故 flushSync 属**防御性**写法（成本为零，防的是并发渲染让出或 React 改调度）；
    - 像素证据那一节是补这块短板的：实测「给 old 快照加 z-index 盖住 new」这条
      （网上常见写法，方向搞反就是它）**只有像素检查抓得到** —— 圆照常张开、
      半径照常插值，其余断言全绿，但屏幕上压根没有颜色变化。
  - 测法踩过的坑：**取样不能用 rAF / 固定 sleep 对齐时间轴** —— headless 下 rAF
    会滞后，整条取样时间轴被推后，「中途」取到的是动画第 0 帧（半径 0，看着像没跑）。
    改成按 `animation.currentTime` 轮询推进；等清理同理改成轮询（view-transition 的
    finished 比动画自身晚一两帧）。

预期效果：换主题不再是莫名其妙的整屏瞬变，颜色从手指点下的那个按钮蔓延开，
「是我刚才那一下点的」这件事看得见。

---

## 5. P6 · 可访问性审计（优先级 P1）

1. **对比度复核**：P1 只做了系别色，整体过一遍 `base.css` 语义色与正文色在
   深/亮两主题下的对比度（工具：axe 或手动算），目标正文 ≥4.5:1、大字 ≥3:1；
2. **ARIA 标签**：给图标按钮（全屏/主题/品牌遮罩）补 `aria-label`/`aria-pressed`
   （现靠 `title`，读屏不读）；`TopNav` 当前项补 `aria-current="page"`；
3. **跳转链接**：`main.content` 前加 skip link（"跳到主内容"），聚焦时显示；
4. **键盘导航全流程走查**：顶栏 → 账号下拉 → 二级菜单 → 列表筛选 → 详情弹窗 →
   底部 tab，逐站确认 Tab/Esc/Enter 可达；
5. **`prefers-reduced-motion` 扩展**：所有新动效（P5 过渡、骨架扫光）都挂
   `@media (prefers-reduced-motion: reduce)` 关闭。

预期效果：读屏与键盘用户完整可用，符合 WCAG A/AA 基本要求，也是后续功能的基线。

---

## 6. P7 · 图片与多媒体管线（优先级 P2）

现状：`internal/gamedata/data/img` 共 2694 张 webp、合计 26MB，单张最大 1.7MB；
`scripts/gen_images.py` 负责 PNG→webp 转码。

1. **转码参数优化**：`gen_images.py` 给 webp 编码加质量上限（如 `-q 80`）与
   最长边限制（如 512px，头像类本就不需要原尺寸），目标单张 ≤200KB、总量下降 30%+；
2. **压缩核对**：转码后 `du -sh` 对比，挑最大若干张人工看质量损失；
3. **格式决策**：webp 已是局域网内最优选（无需 AVIF 兼容矩阵）；保持 embed 单二进制；
4. **加载侧配合**：头像首屏 `fetchpriority="high"`，其余保持 `loading="lazy"`；
   图片占位灰底（P5 已有）避免空白闪动；
5. 注意：改 `gen_images.py` 属**后端/生成脚本区域**，且重跑会动
   `internal/gamedata/data/img/`（生成物），需与后端 owner 协调时机，避免与
   对方未提交改动冲突。

预期效果：整体流量与内存占用下降，首屏图片更快就位；质量损失肉眼不可见。

---

## 7. P8 · 页面布局与响应式补强（优先级 P2）

1. **平板断点审计**：`list.css`/`detail.css`/`map.css` 逐页过 768–1024px 表现，
   补 `@media` 断点（导航收窄、卡片列数调整）；
2. **内容层级**：各列表页统一"筛选区 → 统计条 → 列表 → 空态"，空态补统一
   插画/文案组件；
3. **导航结构**：`TopNav` 页面增多后可考虑分组或折叠（现在 8 项），移动端底部
   tab 同样过一遍溢出场景；
4. **超宽屏**：2xl 下各页内容居中与 `--reading-max` 对齐。

预期效果：所有常用分辨率下布局无破损、无横向滚动，层级清晰。

---

## 8. 验收清单（每阶段通用）

- [ ] `cd web && npm run build` 通过，产物进 `internal/server/web/`
- [ ] 深/亮两主题各过一遍主要页面
- [ ] `prefers-reduced-motion` 下无动画残留
- [ ] 键盘 Tab 全流程可达、焦点不丢失
- [ ] 若动 `internal/server/`、`scripts/`、生成物或 `docs/`：先 `git status`
      确认无他人未提交改动，并遵守 AGENTS.md 区域归属（后端/生成脚本改动需
      与对应 owner 打招呼）
- [ ] 后端改动后跑 `UPDATE_CONTRACT=1 go test ./internal/server/ -run TestContract`
      （若动了响应结构）

## 9. 顺序与依赖

```
P3（分包/缓存） → P4（字体/排版） → P5（过渡/骨架/焦点）
     └──────────────┬──────────────────┘
                    ↓
P6（可访问性审计，可与 P5 并行）→ P7（图片管线，需后端协调）→ P8（响应式收尾）
```
