// 图鉴主题(handbook)的贴图获取:从洛克图鉴取游戏原生 UI 贴图到 web/public/theme/handbook/。
//
//   node scripts/fetch-theme-textures.mjs            # 幂等:已存在则跳过
//   node scripts/fetch-theme-textures.mjs --force    # 全部重取
//
// ⚠️ 来源说明与合规(与根 scripts/fetch_bigmap_hd.py 同一口径,务必连同本注释一起看):
//   - 这些**不是**我们自己的解包产物,而是从第三方粉丝站 https://roco.errorawa.dpdns.org/
//     直接下载的。它们本身是游戏原生贴图(该站 index.html 底部注释标了 pak 内原始路径,
//     如 `NRC/Content/NewRoco/Modules/System/Handbook/Raw/**`),但**取用路径是第三方站**。
//   - 只作过渡:正路是自己跑 scripts/unpack.sh 解包,再给 scripts/gen_icons.py 加一个
//     handbook 组产出同名 webp。届时把本脚本的输出目录与产物格式一并对齐,即可整体替换,
//     主题侧无需改动(引用的是 /theme/handbook/<名字> 这个稳定约定)。
//   - 因此本脚本刻意保持「一张表 + 幂等下载」的简单形态:它是可替换的,不该长出复杂逻辑。
//
// 为什么要写脚本而不是手工丢文件:来源可追溯、可重跑、可替换,且新增贴图时只有一处要改。
import { mkdirSync, existsSync, writeFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const OUT = join(ROOT, 'public', 'theme', 'handbook')
const ORIGIN = 'https://roco.errorawa.dpdns.org/'
const FORCE = process.argv.includes('--force')

// [远端路径(相对站点根), 落地相对路径, 用途]
// 「用途」不只是注释:它记录了每张贴图被哪条 CSS 规则引用,改主题时靠它对账。
//
// ⚠️ 只收「真有挂载点」的贴图。参考站还有一批游戏美术(handbook/book/{0,1,2}.png 地区书封、
// handbook/texture/item/*.png 收集进度图标、itemBack.png 星形槽位),它们是**那个站内容页**的
// 素材:书封是蓝色(进这套暖色主题会当场破色),收集图标对应的「收集进度」面板我们也没有。
// 与其下一堆用不上的二进制进构建产物,不如只留三张 —— 等真有对应页面时再加,加一行即可。
const FILES = [
  // 页面底纹:整幅羊皮纸 + 淡刻线稿,做 body 的固定底图(见 handbook.css)
  ['handbook/texture/SideBar.png', 'texture/SideBar.png', '页面底纹'],
  // 金锦带(右端带缺口):顶栏激活项与选中卡片的条带
  ['handbook/texture/select.png', 'texture/select.png', '锦带:顶栏激活项 / 选中卡片'],
  // 「已达成」印章:培育线卡片的状态徽章(.br-status.s-done)
  ['handbook/texture/finish.png', 'texture/finish.png', '已达成印章'],
]

// PNG 魔数:远端挂掉时 CDN 常返回一个 HTML 错误页,只看 HTTP 200 会把它当图片存下来,
// 到浏览器里才表现为「图裂」——这里当场挡住。
const isPng = (buf) =>
  buf.length > 8 && buf[0] === 0x89 && buf[1] === 0x50 && buf[2] === 0x4e && buf[3] === 0x47

let fail = 0
let got = 0
let kept = 0

console.log(`=== 图鉴主题贴图 ${FORCE ? '(强制重取)' : '(幂等)'} ===`)
console.log(`来源: ${ORIGIN}\n`)

for (const [remote, rel, use] of FILES) {
  const dest = join(OUT, rel)
  if (!FORCE && existsSync(dest)) {
    kept++
    console.log(`  跳过 ${rel.padEnd(22)} 已存在(--force 可重取)`)
    continue
  }
  let res
  // 单个文件失败不该中断整批 —— 但必须计入失败并让退出码非 0,否则「少了几张」会被静默带过。
  try {
    res = await fetch(ORIGIN + remote, { redirect: 'follow' })
  } catch (e) {
    fail++
    console.error(`  ✗ ${rel.padEnd(22)} 请求失败: ${e.message}`)
    continue
  }
  if (!res.ok) {
    fail++
    console.error(`  ✗ ${rel.padEnd(22)} HTTP ${res.status}  (${remote})`)
    continue
  }
  const buf = Buffer.from(await res.arrayBuffer())
  if (!isPng(buf)) {
    fail++
    console.error(`  ✗ ${rel.padEnd(22)} 不是 PNG(${buf.length} 字节,可能是错误页)—— 不写入`)
    continue
  }
  mkdirSync(dirname(dest), { recursive: true })
  writeFileSync(dest, buf)
  got++
  console.log(`  ✓ ${rel.padEnd(22)} ${String(Math.round(buf.length / 1024)).padStart(4)}KB  ${use}`)
}

// 落盘后复核一遍清单:上面的循环只保证「这次写进去的对」,不保证「该有的都在」
// (比如上次跑到一半中断过)。对账才让这个脚本可以无脑重跑。
const missing = FILES.filter(([, rel]) => !existsSync(join(OUT, rel)))
console.log(`\n新取 ${got} 张,跳过 ${kept} 张,失败 ${fail} 张`)
if (missing.length) console.log(`⚠️ 清单里仍缺: ${missing.map(([, r]) => r).join(' ')}`)
console.log(fail || missing.length ? '✗ 未取全' : `✓ 全部就位(${FILES.length} 张)@ ${OUT}`)
process.exit(fail || missing.length ? 1 : 0)
