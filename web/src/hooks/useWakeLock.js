import { useEffect } from 'react'

// 屏幕常亮是否可用:仅 secure context(HTTPS/localhost)下浏览器才暴露 wakeLock。
export const wakeLockSupported = 'wakeLock' in navigator

// useWakeLock 按开关请求屏幕常亮锁,阻止设备熄屏/降亮。
// 切到后台锁会被系统自动释放,回到前台需重新获取(visibilitychange)。
export function useWakeLock(enabled) {
  useEffect(() => {
    if (!enabled || !wakeLockSupported) return
    let lock = null
    const acquire = async () => {
      try { lock = await navigator.wakeLock.request('screen') } catch { /* 拒绝/不可用则静默 */ }
    }
    const onVis = () => { if (document.visibilityState === 'visible') acquire() }
    acquire()
    document.addEventListener('visibilitychange', onVis)
    return () => {
      document.removeEventListener('visibilitychange', onVis)
      lock?.release().catch(() => {})
    }
  }, [enabled])
}
