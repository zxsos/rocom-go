import { useEffect, useRef, useState } from 'react'

// 开屏加载动画:结构、时序、素材都取自洛克图鉴(roco.errorawa.dpdns.org)的 #loading 层,
// 图片在 web/public/loading/,样式在 styles/splash.css。这里只做三件事:
//   1. 按原站 mustLoad() 的办法预加载开屏素材,进度条(书脊上那根绿枝)按「已加载张数」生长;
//   2. ready(首屏数据到位)后加 .finish 播片 —— 叶片弹出、光点绽放、精灵图光效扫一遍;
//   3. 同时加 .hide 退场,1.7s 后卸载(时长对齐 CSS:0.7s 延时 + 1s 淡出)。
//
// 与原站的两处不同,都是为了「不把外壳一起拖下水」:
//   - 原站以「数据脚本全部执行完」为完成信号;这里由调用方传 ready,只认首屏必需的那两个请求。
//   - 原站失败时另有 .loadFailed 分支 + 重试按钮;这里不拦路,MAX_MS 到点无条件放行
//     (开屏是装饰,不该因为它自己挂掉而把整个应用挡在门外)。
const BASE = '/loading/'

// 第一层(书本)里的「静止叶」。数组是 [原站类名, 素材名] —— 右侧三片叶子的素材名
// 与类名不同名(rightTopLeaf2 → rightTop2),故不能按类名直接拼路径。
const STATIC_LEAVES = [
  ['leftTop2', 'leftTop2'],
  ['leftTop1', 'leftTop1'],
  ['left2', 'left2'],
  ['left1', 'left1'],
  ['top4', 'top4'],
  ['top3', 'top3'],
  ['top2', 'top2'],
  ['top1', 'top1'],
  ['rightTopLeaf3', 'rightTop3'],
  ['rightTopLeaf2', 'rightTop2'],
  ['rightTopLeaf1', 'rightTop1'],
  ['rightLeaf', 'right'],
]

// 飞舞叶:序号即 CSS 类名 flyLeaf<N>(各有独立关键帧),素材是重复借用的那几片。
const FLY_LEAVES = [
  ['flyLeaf8', 'top4'],
  ['flyLeaf7', 'leftTop2'],
  ['flyLeaf6', 'rightTop3'],
  ['flyLeaf5', 'leftTop2'],
  ['flyLeaf4', 'top4'],
  ['flyLeaf3', 'top4'],
  ['flyLeaf2', 'rightTop3'],
  ['flyLeaf1', 'leftTop2'],
]

// 光点:19 个,每个独立关键帧(round1-19),位置/缩放/时长/延时都不同 —— 原站就是手调 19 份,
// 没有可参数化的规律。DOM 顺序与原站一致(19 先,1 后)。
const ROUNDS = Array.from({ length: 19 }, (_, i) => `round${19 - i}`)

// 预加载清单 = 原站 mustLoad 里 loading/ 那一批(18 张)。进度条按它算百分比。
const PRELOAD = [
  'back.png',
  'front.png',
  'progress.png',
  'finishProgress.png',
  'light.png',
  'round.png',
  ...[...new Set([...STATIC_LEAVES, ...FLY_LEAVES].map(([, name]) => name))].map((n) => `leaf/${n}.png`),
]

const MIN_MS = 900 // 最短停留:素材命中缓存时也要看得见进度条走满,否则一闪而过像闪屏
const HOLD_MS = 340 // 进度走满后的停顿(原站是 setTimeout 500,留一点给「满了」这个状态被看见)
const MAX_MS = 8000 // 硬超时:接口不返回也必须进场
const EXIT_MS = 1700 // 退场总时长,与 CSS 对齐(loaded1 的 0.7s 延时 + 1s 淡出)
const EXIT_REDUCED_MS = 320 // 降级版退场:只剩 CSS 里那 .3s 淡出,多留 20ms 保证动画播完再摘

// 系统是否要求减少动态效果。开屏是 1.7s 的纯装饰播片(12 片叶弹出 + 8 片飞散 + 19 个光点 +
// 精灵图光效),对光敏感人群是强刺激,必须能整段跳过 —— 只保留一次极短淡出。
// 进度条**不降级**:它是「正在加载」的实质信息,与 base.css 对 .spinner 的处置一致(降速不停止)。
const reduceMotion = () =>
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches

export default function Splash({ ready }) {
  const [progress, setProgress] = useState(0)
  const [phase, setPhase] = useState('loading') // loading → finish(播片+退场) → done(卸载)
  const [forced, setForced] = useState(false)
  const startRef = useRef(Date.now())
  // 渲染期读一次即可:偏好不会中途变,真变了也是下次刷新生效,不值得为它挂 listener
  const [reduce] = useState(reduceMotion)
  const exitMs = reduce ? EXIT_REDUCED_MS : EXIT_MS

  // 预加载:每张图(load 或 error)都算一格 —— 原站也是 onerror 计入,
  // 否则缺一张图进度条就永远到不了 100,开屏变成死等。
  useEffect(() => {
    let done = 0
    let cancelled = false
    const total = PRELOAD.length
    const bump = () => {
      if (cancelled) return
      done += 1
      setProgress(Math.min(100, (done / total) * 100))
    }
    PRELOAD.forEach((name) => {
      const img = new Image()
      img.onload = bump
      img.onerror = bump
      img.src = BASE + name
    })
    return () => {
      cancelled = true
    }
  }, [])

  // 硬超时兜底:进度条也按满格处理,免得卡在 62% 直接跳走
  useEffect(() => {
    const t = setTimeout(() => {
      setProgress(100)
      setForced(true)
    }, MAX_MS)
    return () => clearTimeout(t)
  }, [])

  const canGo = (progress >= 100 && ready) || forced

  // 起播:等进度满 + 数据就绪,再按最短停留补齐剩余时间(两者取大)
  useEffect(() => {
    if (phase !== 'loading' || !canGo) return
    const wait = Math.max(HOLD_MS, MIN_MS - (Date.now() - startRef.current))
    const t = setTimeout(() => setPhase('finish'), wait)
    return () => clearTimeout(t)
  }, [phase, canGo])

  // 退场:到点卸载。中间不改任何状态 —— .hide/.finish 都只是加类,动画全在 CSS 里跑。
  // exitMs 与 splash.css 的 loaded1 是一对约束(1.7s / 降级 .3s),改一处必须同步另一处。
  useEffect(() => {
    if (phase !== 'finish') return
    const t = setTimeout(() => setPhase('done'), exitMs)
    return () => clearTimeout(t)
  }, [phase, exitMs])

  if (phase === 'done') return null

  // 降级态刻意**不加 .finish**:那些关键帧(finishLeaf / flyLeaf* / round*)全挂在
  // `#loading.finish` 下,不加类叶片就停在基础态 scale(0),等于整段播片自然跳过,
  // 不必再让每条规则各写一份降级。splash.css 末尾另有一层兜底。
  const exitClass = phase === 'finish' ? (reduce ? 'hide' : 'hide finish') : ''

  return (
    <div id="loading" className={exitClass} role="status" aria-label="加载中">
      <div className="center">
        {STATIC_LEAVES.map(([cls, src]) => (
          <div className={cls} key={cls}>
            <img src={BASE + 'leaf/' + src + '.png'} alt="" draggable={false} />
          </div>
        ))}
        <div className="back">
          <img src={BASE + 'back.png'} alt="" draggable={false} />
        </div>
        {/* 进度条:内部 div 的 height 就是进度。.box 有 transform: scale(-1),
            所以它是「从下往上」长的(见 splash.css)。 */}
        <div className="progress">
          <div className="box">
            <div style={{ height: progress + '%' }}>
              <img className="loadingProgress" src={BASE + 'progress.png'} alt="" draggable={false} />
              <img className="finishProgress" src={BASE + 'finishProgress.png'} alt="" draggable={false} />
            </div>
          </div>
        </div>
        <div className="front">
          <img src={BASE + 'front.png'} alt="" draggable={false} />
        </div>
        {FLY_LEAVES.map(([cls, src]) => (
          <div className={cls} key={cls}>
            <img src={BASE + 'leaf/' + src + '.png'} alt="" draggable={false} />
          </div>
        ))}
        <div className="light">
          <div />
        </div>
      </div>
      {/* 第二个 .center 叠在第一层之上:19 个光点是独立元素(而非一张图),
          各自的关键帧负责把它从中心弹到书脊周围。 */}
      <div className="center">
        {ROUNDS.map((cls) => (
          <div className={cls} key={cls}>
            <img src={BASE + 'round.png'} alt="" draggable={false} />
          </div>
        ))}
      </div>
    </div>
  )
}
