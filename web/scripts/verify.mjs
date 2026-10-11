// 前端验收统一入口:把散在 package.json 里的几十个 verify-*.mjs 收成一组。
//
//   node scripts/verify.mjs [unit|served|browser|all] [--only <子串>]
//
// 各脚本保持独立(单个仍可 node scripts/verify-xxx.mjs 直跑),这里只负责按组
// 顺序执行并汇总结果。失败的脚本名会列在最后,退出码非零。
//
// 组划分(与原 npm scripts 对应):
//   unit    —— 纯 node/jsdom 级检查,不依赖后端与浏览器,最快(原 `npm run verify`)
//   served  —— 需要后端在跑(默认 http://localhost:4939,可用 VERIFY_BACKEND 覆盖)
//   browser —— playwright 真浏览器,需要先 `npm run build` 并启动后端
//   all     —— unit + served + browser
//
// 注意 served/browser 组的前置条件(后端/构建产物)由调用方保证,本脚本不代启动,
// 跑不起来时看各脚本头注释的「前置」说明。

import { spawnSync } from 'node:child_process'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const WEBROOT = join(dirname(fileURLToPath(import.meta.url)), '..')

// [脚本, 附加参数[]]
const GROUPS = {
  unit: [
    ['check-css-vars.mjs'],
    ['verify-rules.mjs'],
    ['verify-slider.mjs'],
    ['verify-motion.mjs'],
    ['verify-motion-enter.mjs'],
    ['verify-splash.mjs'],
    ['verify-pet-views.mjs'],
    ['verify-filter-count.mjs'],
    ['verify-ctx-menu.mjs'],
    ['verify-pager-dropdown.mjs'],
    ['verify-toggle-off-state.mjs'],
    ['verify-map-vp.mjs'],
    ['verify-pip.mjs'],
    ['verify-subscribe.mjs'],
    ['verify-sigil.mjs'],
    ['verify-theme-spread.mjs'],
    ['verify-theme-handbook.mjs'],
    ['verify-breeding-goal-nature.mjs'],
    ['verify-breeding-goal-progress.mjs'],
    ['verify-breeding-page.mjs'],
    ['verify-breeding-newlink.mjs'],
    ['verify-breeding-parents.mjs'],
    ['verify-egggroup-ui.mjs'],
  ],
  served: [
    ['verify-pages.mjs'],
    ['verify-switch.mjs'],
    ['verify-sse.mjs'],
  ],
  browser: [
    ['verify-page-boot.mjs'],
    ['verify-route-enter.mjs'],
    ['verify-map-vp-browser.mjs'],
    ['verify-map-browser.mjs'],
    ['verify-map-motion-browser.mjs', ['tap-move']],
    ['verify-pip-canvas.mjs'],
    ['verify-admin-browser.mjs'],
    ['verify-admin-paging.mjs'],
    ['verify-account-width.mjs'],
    ['verify-account-mobile.mjs'],
    ['verify-pet-card-size.mjs'],
    ['verify-pet-toggles.mjs'],
  ],
}

const argv = process.argv.slice(2)
const groupArg = argv.find((a) => !a.startsWith('--')) || 'unit'
const onlyIdx = argv.indexOf('--only')
const only = onlyIdx >= 0 ? argv[onlyIdx + 1] : ''

const groups = groupArg === 'all' ? ['unit', 'served', 'browser'] : [groupArg]
for (const g of groups) {
  if (!GROUPS[g]) {
    console.error(`未知组: ${g},可选 unit|served|browser|all`)
    process.exit(2)
  }
}

let pass = 0
const failed = []
const t0 = Date.now()
for (const g of groups) {
  console.log(`\n=== ${g} ===`)
  for (const [script, args = []] of GROUPS[g]) {
    if (only && !script.includes(only)) continue
    process.stdout.write(`▶ ${script} ... `)
    const r = spawnSync('node', [`scripts/${script}`, ...args], {
      cwd: WEBROOT, stdio: 'pipe', encoding: 'utf8',
    })
    if (r.status === 0) {
      pass++
      console.log('✓')
    } else {
      failed.push(script)
      console.log('✗')
      // 只吐尾部输出,免得一个失败刷屏淹没汇总
      const out = (r.stdout + r.stderr).trim().split('\n')
      for (const line of out.slice(-15)) console.log('    ' + line)
    }
  }
}

console.log(`\n—— 共 ${pass + failed.length} 项,通过 ${pass},失败 ${failed.length},` +
  `耗时 ${((Date.now() - t0) / 1000).toFixed(1)}s ——`)
if (failed.length) {
  console.log('失败: ' + failed.join(', '))
  process.exit(1)
}
console.log('✓ 全部通过')
