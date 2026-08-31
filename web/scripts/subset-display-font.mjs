// 中文标题显示字体子集化(可复现脚本,前端区域维护)。
//
// 输入:web/src 里的 h1/h2 标题文本 + 宠物名(internal/gamedata/data/names.json,
//       详情弹窗 h2 显示宠物名,须一并打包)+ 品牌词 + ASCII。
// 输出:web/public/fonts/source-han-sans-sc-700-subset.woff2(SIL OFL 1.1)。
//
// 为何子集:源字体 Noto Sans CJK SC Bold 约 17MB,标题字体只需其中 ~950 字
// (标题 20 字 + 宠物名 830 字 + ASCII/标点),woff2 后 ~238KB。
// 选 700 一档:标题全部走粗体(站名/页面标题/宠物名),无 regular 展示场景。
// 子集外的字符(新标题、新宠物名)按 @font-face 的 unicode-range + 回退链
// 逐字符回退系统字体,不出现豆腐块 —— 所以新增标题文字后需重跑本脚本。
//
// 用法:
//   node web/scripts/subset-display-font.mjs            # 需本地有源字体(见下)
//   node web/scripts/subset-display-font.mjs --font /path/to/NotoSansCJKsc-Bold.otf
//   node web/scripts/subset-display-font.mjs --download # 从 GitHub 下载源字体到 /tmp
//
// 依赖:uv + fonttools + brotli(pyftsubset 经 uv run 临时环境调用,不污染项目依赖)。

import { readFileSync, readdirSync, existsSync, writeFileSync, mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..')
const SRC_DIR = join(ROOT, 'web', 'src')
const OUT = join(ROOT, 'web', 'public', 'fonts', 'source-han-sans-sc-700-subset.woff2')
const SOURCE_URL = 'https://github.com/googlefonts/noto-cjk/raw/main/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Bold.otf'

const args = process.argv.slice(2)
const download = args.includes('--download')
const fontArgIdx = args.indexOf('--font')
const fontArg = fontArgIdx >= 0 ? args[fontArgIdx + 1] : null

// ---- 1. 收集字符集 ----

const chars = new Set()

// 1a. h1/h2 标题文本(含 JSX 表达式字面量里的中文,如 {cond ? '管理员登录' : '设置密码'})
const headingRe = /<h[12][^>]*>([\s\S]*?)<\/h[12]>/g
const cjkRe = /[\u4e00-\u9fff\u3000-\u303f\uff00-\uffef]/
for (const f of readdirSync(SRC_DIR, { recursive: true })) {
  if (typeof f !== 'string' || !f.endsWith('.jsx')) continue
  const src = readFileSync(join(SRC_DIR, f), 'utf8')
  for (const m of src.matchAll(headingRe)) {
    for (const ch of m[1]) if (cjkRe.test(ch)) chars.add(ch)
  }
}

// 1b. 品牌词(顶栏站名「妙妙屋」等)
for (const w of '妙妙屋洛克王国世界') chars.add(w)

// 1c. 宠物名:详情弹窗 h2 = pet.name||pet.species,是显示字体的主角
const names = JSON.parse(readFileSync(join(ROOT, 'internal', 'gamedata', 'data', 'names.json'), 'utf8'))
for (const v of Object.values(names.species ?? {})) {
  for (const ch of String(v)) if (/[\u4e00-\u9fff]/.test(ch)) chars.add(ch)
}

// 1d. ASCII 可打印 + 标题里可能出现的中西文标点
for (let c = 0x20; c <= 0x7e; c++) chars.add(String.fromCharCode(c))
for (const ch of '\u00b0\u00d7\u2026\u2014\u2018\u2019\u201c\u201d') chars.add(ch)

const text = [...chars].sort().join('')

// ---- 2. 定位源字体 ----

let srcFont = fontArg
if (!srcFont && download) {
  srcFont = join(tmpdir(), 'NotoSansCJKsc-Bold.otf')
  if (!existsSync(srcFont)) {
    console.log(`下载源字体 → ${srcFont}`)
    execFileSync('curl', ['-sL', '--max-time', '300', '-o', srcFont, SOURCE_URL], { stdio: 'inherit' })
  }
}
if (!srcFont || !existsSync(srcFont)) {
  console.error(`找不到源字体(共 ${text.length} 字符待子集)。请用 --font <路径> 指定 17MB 的 NotoSansCJKsc-Bold.otf,或加 --download 从 GitHub 拉取。`)
  process.exit(1)
}

// ---- 3. 子集化 ----

const tmpChars = join(mkdtempSync(join(tmpdir(), 'rocom-font-')), 'chars.txt')
writeFileSync(tmpChars, text, 'utf8')
console.log(`子集字符数:${text.length} → ${OUT}`)
execFileSync('uv', [
  'run', '--with', 'fonttools', '--with', 'brotli',
  'pyftsubset', srcFont,
  '--text-file', tmpChars,
  '--flavor=woff2',
  '--output-file', OUT,
  '--no-hinting', '--desubroutinize',
], { stdio: 'inherit' })
console.log('完成。检查体积:du -h ' + OUT)
