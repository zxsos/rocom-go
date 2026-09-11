// 临时 mock:用**真组件 + 真 CSS**渲染代数多了之后的培育页,只为看界面。
// 不属于应用、不进构建产物(vite build 只吃 index.html),看完即删。
//
// 数据按**新模型**造:线的身份是种母,故同一条线里历代的**种母是固定的**,变的只有种公 ——
// 换种母会另开子线(见 internal/pet.NewChildLine)。所以这里是一条 10 代的母线 + 一条接班的子线。
import React from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { AccountContext } from '../src/context'
import '../src/styles/base.css'
import '../src/styles/breeding.css'
import '../src/styles/panel.css'
import '../src/styles/detail.css'
import '../src/styles/list.css'
import '../src/styles/dropdown.css'
import '../src/styles/motion.css'
import LineDetail from '../src/pages/breeding/LineDetail'
import LineCard from '../src/pages/breeding/LineCard'

const HEAD = 'HeadIcon/3006.webp'
const snap = (gid, name, voice, weight, gender) => ({
  gid, name, species: '火神', confId: 2000672, gender, img: HEAD,
  heightM: 1.28, weightKg: 45.6, heightPct: 61.42, weightPct: weight,
  voice, nature: '固执', talentRank: 'A',
})

// 母线:种母固定为 V90 W80 的「小母」,10 代换 10 只种公。子代 = floor((母+父)/2),与后端
// Predict 同口径 —— 数字自洽,不至于出现配不出来的值。
const MOM = { voice: 90, weight: 80 }
const FATHERS = [62, 68, 74, 80, 84, 88, 92, 96, 98, 100]
const FW = [55.2, 61.0, 66.8, 72.5, 77.1, 81.6, 85.9, 90.3, 94.0, 97.2]

const gens = FATHERS.map((fv, i) => {
  const gen = i + 1
  const cv = Math.floor((MOM.voice + fv) / 2)
  const cw = (MOM.weight + FW[i]) / 2
  return {
    gen,
    mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'),
    father: snap(2100 + i, `种公${gen}`, fv, FW[i], '♂'),
    child: snap(3000 + i, `子代${gen}`, cv, cw, gen === 8 || gen === 9 ? '♀' : '♂'),
    source: 'auto',
    at: 1750000000 + i * 86400,
    eggGid: 5000 + i,
  }
})

// 第 11 代待孵(蛋还在窝上)、第 12 代待认领(破壳了,子代还没进背包)
const pending = [
  {
    gen: 11, mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'),
    father: snap(2110, '种公11', 100, 98.4, '♂'),
    source: 'auto', at: 1750000000 + 10 * 86400, eggGid: 5011,
  },
  {
    gen: 12, mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'),
    father: snap(2111, '种公12', 98, 97.0, '♂'),
    childGid: 3012, source: 'auto', at: 1750000000 + 12 * 86400, eggGid: 5012,
  },
]

// 蛋:第 11 代那颗还在孵(它有了蛋才不会显示「这颗蛋已经不在背包里了」)
const eggs = {
  5011: {
    gid: 5011, name: '火神的蛋', icon: 'egg/egg_huohua.webp',
    weightKg: 12.4, weightPct: 88.6, voice: 95,
    medals: [
      { dim: 3, name: '声音奖牌', icon: 'badge/voice.webp' },
      { dim: 2, name: '体重奖牌', icon: 'badge/weight.webp' },
    ],
  },
}

const GOAL = { voice: 100, weightPct: 100, nature: '固执' }

// 建议:母本固定,故「换种公」是这一条第 12 代真正要做的决定
const sugExp = (v, w) => ({
  voice: v, weightPct: w, weightHi: Math.min(100, w + 2), natureP: 0.61, natureFrom: 'parent',
})
const suggest = [
  { mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'), father: snap(2120, '甲公', 100, 98.6, '♂'), exp: sugExp(95, 89.3), score: 0.035 },
  { mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'), father: snap(2121, '乙公', 100, 97.2, '♂'), exp: sugExp(95, 88.6), score: 0.041 },
  { mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'), father: snap(2122, '丙公', 99, 99.1, '♂'), exp: sugExp(94, 89.6), score: 0.052, ambiguous: true },
  { mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'), father: snap(3008, '子代8', 91, 85.9, '♀'), exp: sugExp(90, 83.0), score: 0.104, backcross: true },
]

const line = {
  id: 'mock-10gen',
  evo: 1001, species: '火神', confId: 2000672,
  motherGid: 2001,
  mother: snap(2001, '小母', MOM.voice, MOM.weight, '♀'),
  goal: GOAL,
  status: 'active',
  gens, pending, eggs,
  suggest,
  reach: { target: 100, best: 95, hit: false },
  backcross: {
    advised: false,
    reason: '换种更接近目标:现有候选里有比回交更能拉近差距的',
    withParent: snap(2001, '小母', MOM.voice, MOM.weight, '♀'),
    exp: sugExp(90, 83.0),
    alt: snap(2120, '甲公', 100, 98.6, '♂'),
    altExp: sugExp(95, 89.3),
  },
  chainName: '火神',
  createdAt: 1750000000, updatedAt: 1750000000 + 12 * 86400,
}

// 接班的子线:第 9 代那只 ♀ 子代(V93 W85.9)当新母本,代数接着母线数(GenBase=10 → 从 11 起)。
// 这正是「换种母自动开子线」的结果 —— 谱系条把它和母线连起来看。
const KIDMOM = { voice: 93, weight: 85.9 }
const kidGens = [11, 12].map((gen, i) => {
  const fv = [100, 98][i]
  const fw = [99.4, 97.8][i]
  return {
    gen,
    mother: snap(3008, '子代9', KIDMOM.voice, KIDMOM.weight, '♀'),
    father: snap(2130 + i, `接班种公${i + 1}`, fv, fw, '♂'),
    child: snap(3100 + i, `接班子代${i + 1}`, Math.floor((KIDMOM.voice + fv) / 2), (KIDMOM.weight + fw) / 2, '♂'),
    source: 'auto', at: 1750000000 + (13 + i) * 86400, eggGid: 5100 + i,
  }
})
const childLine = {
  ...line,
  id: 'mock-child',
  parentLineId: 'mock-10gen',
  motherGid: 3008,
  mother: snap(3008, '子代9', KIDMOM.voice, KIDMOM.weight, '♀'),
  genBase: 10,
  gens: kidGens, pending: [], eggs: {}, suggest: [], backcross: null, reach: null,
  updatedAt: 1750000000 + 15 * 86400,
}

const MATRIX = [
  ['', '大胆', '固执', '调皮', '勇敢', '逞强'],
  ['稳重', '', '天真', '懒散', '悠闲', '坦率'],
  ['聪明', '专注', '', '偏执', '冷静', '理性'],
  ['警惕', '温顺', '坚毅', '', '马虎', '认真'],
  ['胆小', '急躁', '开朗', '莽撞', '', '热情'],
  ['沉默', '忧郁', '平和', '浮躁', '勤劳', ''],
]

// 候选池接口 stub:补录面板要它。真数据没有,给空数组即可(不报错就行)。
const origFetch = window.fetch
window.fetch = async (url, init) => {
  const u = String(url)
  if (u.includes('/api/breeding/pool')) {
    const picked = [snap(2001, '小母', MOM.voice, MOM.weight, '♀')]
    return new Response(JSON.stringify({
      evo: 1001, species: '火神',
      mothers: picked,
      fathers: [snap(2120, '甲公', 100, 98.6, '♂'), snap(2121, '乙公', 100, 97.2, '♂')],
      kids: gens.map((g) => g.child),
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }
  if (u.includes('/api/name-options')) {
    return new Response(JSON.stringify({ nature: MATRIX, species: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }
  return origFetch(url, init)
}

function Mock() {
  const [id, setId] = React.useState(line.id)
  const cur = id === line.id ? line : childLine
  return (
    <MemoryRouter>
      <AccountContext.Provider value="mock">
        <div className="br-page" style={{ padding: 16, maxWidth: 1280, margin: '0 auto' }}>
          <div className="toolbar">
            <button className="btn small">← 全部培育线</button>
          </div>
          <LineDetail
            line={cur} lines={[line, childLine]} chains={[]} chainItems={[]}
            natureMatrix={MATRIX} loading={false} busy={false}
            onSave={() => {}} onClaim={() => Promise.resolve(false)}
            onRemove={() => {}} onPet={() => {}} onLocate={() => {}}
            onOpenLine={setId} onMerge={() => Promise.resolve(false)}
          />
          <h3 style={{ margin: '24px 0 8px' }}>列表卡片(同一个品种几条线,靠种母区分)</h3>
          <div className="br-grid">
            <LineCard line={line} chains={[]} matrix={MATRIX} onOpen={() => {}} />
            <LineCard line={childLine} chains={[]} matrix={MATRIX} onOpen={() => {}} />
          </div>
        </div>
      </AccountContext.Provider>
    </MemoryRouter>
  )
}

createRoot(document.getElementById('root')).render(<Mock />)
