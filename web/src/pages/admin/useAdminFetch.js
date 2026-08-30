import { useEffect, useRef } from 'react'
import { useAsyncData } from '../../hooks/useAsyncData'

// useAdminFetch 管理面板的数据拉取:在 useAsyncData 之上补一条 401 语义——
// 令牌失效/未登录时通知上层踢回登录页;其余错误(404/500/网络)只展示,不打断面板
// (见 Admin 的 kickIfUnauthed)。
// fetcher 必须引用稳定(用 useCallback 按依赖包一层),否则每次渲染都会重新拉取。
// onUnauthed 走 ref,调用方不必用 useCallback 包裹。
export function useAdminFetch(fetcher, onUnauthed, reloadKey) {
  const { data, error, loading, refresh } = useAsyncData(fetcher, { reloadKey })
  const kickRef = useRef(onUnauthed)
  kickRef.current = onUnauthed
  useEffect(() => {
    // 必须把 error 传给上层:Admin 的 kickIfUnauthed(err) 要读 err.status 判断是不是 401。
    // 漏传会在「令牌失效」这条路上抛 TypeError —— 又因没有 error boundary,整棵树被卸载,
    // 面板闪一下就黑屏,且刷新也回不来(token 还在 localStorage),等于把管理员永久锁死。
    if (error && error.status === 401) kickRef.current?.(error)
  }, [error])
  return { data, error, loading, refresh }
}
