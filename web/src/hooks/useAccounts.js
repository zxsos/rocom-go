import { useCallback, useEffect, useRef, useState } from 'react'
import { getAccounts, getCurrentAccount, setCurrentAccount } from '../api'
import { dropBoxFilter } from '../pages/pet-list/filters'

// 会话内 PIN 解锁标记:按账号存,校验通过一次后本标签页不再重复弹窗。
// key 与后端/其它页面共用(见 PinDialog),改名需同步。
const pinKey = (acc) => 'pin:' + acc
const ADMIN_KEY = 'admin-unlocked'
const unlocked = (acc) =>
  sessionStorage.getItem(pinKey(acc)) === '1' || sessionStorage.getItem(ADMIN_KEY) === '1'

// useAccounts 账号列表与当前账号,含 PIN 策略:
//   - 拉列表并选出默认账号(当前选中项失效时回落到最近活跃的第一个);
//   - 在线状态 15s 轮询(后端按「最近 30s 内有流量」判定,见 server.AccountOnline);
//   - 切账号时被 PIN 拦下 → 通过 onPinRequired 回调交给调用方弹窗,校验通过后再调 select 落地。
//
// 这里只管「策略」,不碰 UI:弹窗长什么样由调用方决定。
// onPinRequired 走 ref,故调用方不必用 useCallback 包裹。
export function useAccounts(onPinRequired) {
  const [accounts, setAccounts] = useState([])
  const [account, setAccount] = useState(getCurrentAccount)
  // 首屏拦截用:默认账号设了 PIN 且本会话未解锁时,由调用方弹窗(不等用户点下拉)。
  const notifyRef = useRef(onPinRequired)
  notifyRef.current = onPinRequired

  const refresh = useCallback(() => {
    getAccounts().then((list) => { if (list) setAccounts(list) }).catch(() => {})
  }, [])

  // 拉账号列表;当前无选中(或选中的已不存在)时默认选最近活跃的第一个。
  // 默认账号若设了 PIN 且未解锁,交给调用方弹窗(首屏即拦截)。
  useEffect(() => {
    getAccounts().then((list) => {
      list = list || []
      setAccounts(list)
      const cur = getCurrentAccount()
      const exists = cur && list.some((a) => a.account === cur)
      const target = exists ? cur : (list.length ? list[0].account : '')
      if (!exists && target) { setCurrentAccount(target); setAccount(target) }
      if (!target) return
      const acc = list.find((a) => a.account === target)
      if (acc?.hasPin && !unlocked(target)) {
        notifyRef.current?.({ account: target, name: acc.name })
      }
    }).catch(() => {})
  }, [])

  // 账号在线状态 15s 轮询:状态不会秒变,15s 足够。仅列表非空时才轮询。
  // 只更新在线标记,不影响当前账号,故不会触发各页重取。
  useEffect(() => {
    if (!accounts.length) return
    const timer = setInterval(refresh, 15000)
    return () => clearInterval(timer)
  }, [accounts.length, refresh])

  // select 直接落地切换(盒子筛选是账号绑定的,换账号必须清掉,否则拿别人的盒 id 去查)。
  const select = useCallback((acc) => {
    setCurrentAccount(acc)
    dropBoxFilter()
    setAccount(acc)
  }, [])

  // request 请求切换:目标账号设了 PIN 且本会话未解锁时返回账号对象(由调用方弹窗),
  // 否则直接切换并返回 null。
  const request = useCallback((acc) => {
    if (!acc || acc === account) return null
    const target = accounts.find((x) => x.account === acc)
    if (target?.hasPin && !unlocked(acc)) return target
    select(acc)
    return null
  }, [account, accounts, select])

  // 当前账号昵称(分享图标题等展示用);未找到时为空串,由使用方兜底。
  const accountName = accounts.find((a) => a.account === account)?.name || ''
  const current = accounts.find((a) => a.account === account)

  return { accounts, account, current, accountName, requestAccount: request, selectAccount: select, refreshAccounts: refresh }
}
