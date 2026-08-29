import { useCallback, useEffect, useRef, useState } from 'react'

// useAsyncData 统一「拉一次远程数据」的骨架:加载中 / 失败 / 重取 / 直接改写,并处理各页
// 原先各写一遍(或干脆漏写)的三件事:
//   - 响应竞态:依赖变化或组件卸载后,迟到的旧响应不得覆盖新数据(世代号守卫);
//   - 出错语义:错误进 error,data 保留上一次的成功值(页面不会因一次抖动而白屏);
//   - 重取时机:fetcher 引用变化即重取;account 这类「不出现在 fetcher 代码里」的隐式依赖,
//     经 reloadKey 显式声明(getXxx 内部读的是 api.js 持有的当前账号)。
// fetcher 必须引用稳定(用 useCallback 包一层),否则每次渲染都会重新拉取。
// 返回 { data, loading, error, refresh, setData }:
//   - refresh 稳定引用,可直接交给 subscribe 的 onOpen 或 useInterval;
//   - setData 供服务端推送直接改写这份状态(快照与增量本就是同一个东西)。
export function useAsyncData(fetcher, { fallback = null, reloadKey } = {}) {
  const [data, setData] = useState(fallback)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const genRef = useRef(0)
  // 用 ref 拿最新 fetcher,refresh 才能保持稳定引用(不随 fetcher 重建而变)。
  const fetchRef = useRef(fetcher)
  fetchRef.current = fetcher

  const refresh = useCallback(() => {
    const gen = ++genRef.current
    setLoading(true)
    setError(null)
    return fetchRef.current().then(
      (d) => {
        if (gen === genRef.current) { setData(d); setLoading(false) }
      },
      (e) => {
        // 失败不清 data:保留上一次成功的结果,页面只多一条错误提示。
        if (gen === genRef.current) { setLoading(false); setError(e) }
      },
    )
  }, [])

  useEffect(() => { refresh() }, [refresh, fetcher, reloadKey])

  return { data, loading, error, refresh, setData }
}

// useAsyncRun 与 useAsyncData 同一套竞态守卫,但只负责「跑一次」:结果交给调用方的
// onDone(自己写 ref、驱动动画、拆成多份 state),本 hook 不托管 data。
// 用于位置快照这类「拉到即就地生效」的场景;reloadKey 语义同 useAsyncData。
// 返回稳定的 run,可直接交给 subscribe 的 onOpen。
export function useAsyncRun(loader, onDone, reloadKey) {
  const genRef = useRef(0)
  const loadRef = useRef(loader)
  const doneRef = useRef(onDone)
  loadRef.current = loader
  doneRef.current = onDone

  const run = useCallback(() => {
    const gen = ++genRef.current
    return Promise.resolve(loadRef.current()).then(
      // 快照失败不报错:等下一次推送(或重连补拉)自然恢复,无需惊动用户。
      (d) => { if (gen === genRef.current && d != null) doneRef.current(d) },
      () => {},
    )
  }, [])

  useEffect(() => { run() }, [run, reloadKey])

  return run
}

// useInterval 每 delay 毫秒调用一次 callback;delay 为 0/null 时不启动。
// callback 取最新引用,重建不会重置计时(省掉各页「把 load 塞进 ref」的写法)。
export function useInterval(callback, delay) {
  const cbRef = useRef(callback)
  cbRef.current = callback
  useEffect(() => {
    if (!delay) return
    const id = setInterval(() => cbRef.current(), delay)
    return () => clearInterval(id)
  }, [delay])
}
