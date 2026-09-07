import React, { useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { getHomeQuery } from '../../api'
import { AccountContext } from '../../context'
import { imgURL } from '../../components/icons'
import { IconHome, IconRefresh } from '../../components/svg'
import { fmtTime } from '../../utils/format'

// 家园查询页:输入 uid 查任意玩家的家园快照。
//
// 与「实时地图」里的小窝图层是两回事:那个来自抓包(只有自己的家园),
// 这个回源第三方(谁都能查)。故本页不接 SSE —— 但**会跟着当前账号走**:
// 地址栏没带 uid 时自动查当前选中的账号(玩家打开页面多半就是想看自己的),
// 带了自己指定的 uid 则永远以地址栏为准,刷新/分享都落在它身上
// (见 useSearchParams)。
//
// 上游是免费站、没有契约保证,故这里的失败是**预期内**的:出错只在本页提示,
// 不 toast、不影响其他页面。

// uidOfAccount 从账号键 "UID:<user_id>" 取出 user_id。
// 与 nav.js 的 uidOf 同口径,不直接引它是为了本页不依赖导航数据文件。
const uidOfAccount = (acc) => {
  const m = /^UID:(\d+)/.exec(acc || '')
  return m ? m[1] : ''
}

// 三种驻守状态的视觉语义。**枚举按 status 文本匹配**,与后端 homeStatusName 同源:
//   - 未喂食:待办,暖色提醒
//   - 已喂食:已完成,中性
//   - 可收取灵感:有收益可拿,高亮(这是玩家真正要找的那只)
const STATUS_KIND = {
  未喂食: 'todo',
  已喂食: 'done',
  可收取灵感: 'ready',
}

// ripeText 把成熟时刻说成人话。
// 只给相对描述、不给「是否已成熟」的布尔判断:后端不确定上游 state 的语义,
// 与其猜错不如把时刻摆出来让人自己看(同源的犹豫见后端 homeQueryPlant 的注释)。
function ripeText(ts) {
  if (!ts) return '—'
  const diff = ts - Date.now() / 1000
  const abs = Math.abs(diff)
  const t =
    abs < 3600 ? Math.max(1, Math.round(abs / 60)) + ' 分钟'
    : abs < 86400 ? Math.round(abs / 3600) + ' 小时'
    : Math.round(abs / 86400) + ' 天'
  return diff > 0 ? `还有 ${t}` : `已过 ${t}`
}

export default function HomeQuery() {
  const [params, setParams] = useSearchParams()
  const urlUID = (params.get('uid') || '').trim()
  const account = useContext(AccountContext)
  // 当前账号自己的 uid;后端还没抓到任何账号时为空串(此时只能手输)。
  const myUID = useMemo(() => uidOfAccount(account), [account])

  // 查谁:地址栏优先,没有就回落到当前账号(打开页面自动看自己的)。
  const targetUID = urlUID || myUID

  const [input, setInput] = useState(targetUID)
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const run = useCallback(async (uid, force) => {
    setLoading(true)
    setError('')
    try {
      setData(await getHomeQuery(uid, force))
    } catch (e) {
      // 失败不清 data:保留上一次结果,页面只多一条错误提示(与 useAsyncData 同策)。
      setError(e.message || '查询失败')
    } finally {
      setLoading(false)
    }
  }, [])

  // 输入框跟随「查谁」:仅在地址栏没指定时同步(切账号时把框里的旧 uid 换掉),
  // 地址栏有 uid 时不动 —— 玩家正在手动改的输入不该被自动值覆盖。
  useEffect(() => {
    if (!urlUID) setInput(myUID)
  }, [urlUID, myUID])

  // 查询目标变化即查询:含首次带 uid 直接进入、以及自动查自己 / 切账号。
  useEffect(() => {
    if (targetUID) run(targetUID, false)
  }, [targetUID, run])

  const submit = (e) => {
    e.preventDefault()
    const uid = input.trim()
    // 只校验「是纯数字」,**不校验位数**:uid 有 6 位也有 9 位(玩家实测),
    // 位数不是前端能假设的。查不查得到由上游判定,它给的 400/422 已有中文文案。
    if (!/^\d+$/.test(uid)) {
      setError('UID 只能是数字')
      return
    }
    // 与当前查询目标相同时:地址栏没变、effect 不会触发,得手动跑一次;
    // 不同时走地址栏,让这次查询可被刷新与分享。
    if (uid === targetUID) run(uid, false)
    else setParams({ uid })
  }

  // 「可收取灵感」的只数:这是整页最该被一眼看到的信息,提到概览区做一个数字。
  // 其余统计按原样给,不喧宾夺主。
  const readyCount = useMemo(
    () => (data?.pets || []).filter((p) => p.status === '可收取灵感').length,
    [data],
  )

  return (
    <div className="hq-page">
      <header className="hq-head">
        <h2 className="hq-title">
          <IconHome size={18} /> 家园查询
        </h2>
        <p className="hq-note">
          按 UID 查询任意玩家的家园快照 · 默认查当前账号 
        </p>
      </header>

      <form className="hq-form" onSubmit={submit}>
        <input
          className="hq-input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="输入 UID,例如 5678116"
          inputMode="numeric"
          autoComplete="off"
          aria-label="玩家 UID"
        />
        <button className="btn primary hq-go" type="submit" disabled={loading}>
          {loading ? '查询中…' : '查询'}
        </button>
        <button
          className="btn hq-force"
          type="button"
          disabled={loading || !targetUID}
          onClick={() => run(targetUID, true)}
          title="跳过服务端 15 分钟缓存,强制回源"
        >
          <IconRefresh size={15} /> 刷新
        </button>
      </form>

      {error && <div className="hq-msg err">{error}</div>}
      {!error && !data && !loading && (
        <div className="hq-empty">
          <IconHome size={40} />
          {/* 有账号时会自动查自己,走到这里通常是「还没抓到任何账号」 */}
          <p>{myUID ? '暂无数据' : '输入一个 UID 开始查询'}</p>
          <p className="hq-empty-sub">
            {myUID ? '可手动输入其他 UID 查询' : '开始抓包后会自动填入当前账号的 UID'}
          </p>

        </div>
      )}

      {data && (
        <>
          <section className="hq-stats">
            <div className="hq-stat wide">
              <b>{data.homeName || '—'}</b>
              <span>家园名</span>
            </div>
            <div className="hq-stat"><b>{data.homeLevel ?? '—'}</b><span>家园等级</span></div>
            <div className="hq-stat"><b>{data.roomLevel ?? '—'}</b><span>房间等级</span></div>
            <div className="hq-stat"><b>{data.comfort ?? '—'}</b><span>舒适度</span></div>
            <div className="hq-stat"><b>{data.pets?.length ?? 0}</b><span>驻守精灵</span></div>
            <div className="hq-stat"><b>{data.plants?.length ?? 0}</b><span>作物</span></div>
            <div className="hq-stat"><b>{data.exp?.toLocaleString?.() ?? data.exp ?? '—'}</b><span>家园经验</span></div>
            {readyCount > 0 && (
              <div className="hq-stat accent">
                <b>{readyCount}</b>
                <span>待收灵感</span>
              </div>
            )}
          </section>

          {(data.pets?.length > 0) && (
            <section className="hq-block">
              <h3 className="hq-sub">驻守精灵</h3>
              <div className="hq-pets">
                {data.pets.map((p, i) => {
                  const kind = STATUS_KIND[p.status]
                  return (
                    <article
                      className={'hq-pet' + (kind ? ' is-' + kind : '')}
                      key={`${p.base}-${p.form}-${i}`}
                      // 依次浮现;12 张之后不再累加,免得长列表末尾等太久(家园上限 10 只)
                      style={{ animationDelay: `${Math.min(i, 12) * 30}ms` }}
                    >
                      <div className="hq-pet-imgwrap">
                        {p.head
                          ? <img className="hq-pet-img" src={imgURL(p.head)} alt={p.species} loading="lazy" />
                          : <div className="hq-pet-img noimg">?</div>}
                        {p.mutation && <span className="hq-mut">{p.mutation}</span>}
                      </div>
                      <div className="hq-pet-name" title={p.name || p.species}>{p.name || p.species}</div>
                      {p.name && p.name !== p.species && <div className="hq-pet-sp">{p.species}</div>}
                      <div className="hq-pet-lv">
                        <span className="hq-lv">Lv{p.level}</span>
                        {p.gender && (
                          <span className={'hq-sex ' + (p.gender === '雄性' ? 'male' : 'female')}>
                            {p.gender === '雄性' ? '♂' : '♀'}
                          </span>
                        )}
                      </div>
                      {p.status && <span className="hq-status">{p.status}</span>}
                    </article>
                  )
                })}
              </div>
            </section>
          )}

          {(data.plants?.length > 0) && (
            <section className="hq-block">
              <h3 className="hq-sub">种植</h3>
              <div className="hq-table-wrap">
                <table className="hq-table">
                  <thead>
                    <tr><th>种子</th><th>成熟</th><th className="num">产量</th><th className="num">可被偷</th><th className="num">已偷</th></tr>
                  </thead>
                  <tbody>
                    {data.plants.map((p, i) => {
                      const ripe = p.ripeAt > 0 && p.ripeAt <= Date.now() / 1000
                      return (
                        <tr key={i} className={ripe ? 'is-ripe' : ''}>
                          <td>{p.seedName || '—'}</td>
                          <td className="hq-ripe">{ripe ? '已成熟' : ripeText(p.ripeAt)}</td>
                          <td className="num">{p.harvest}</td>
                          <td className="num">{p.canSteal}</td>
                          <td className="num">{p.stolen}</td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </section>
          )}

          <footer className="hq-foot">
            快照时间 {fmtTime(data.fetchedAt)}
            {data.cached && ' · 命中缓存'}
            {data.uid && <> · UID <span className="privacy">{data.uid}</span></>}
          </footer>
        </>
      )}
    </div>
  )
}
