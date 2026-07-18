import { useEffect, useState } from 'react'

// useStoredState 把 state 持久化到 storage[key]:decode 从存储串还原初值(无记录时收到 null),
// encode 序列化写回。存储格式由调用方给定,与既有键保持兼容。
export function useStoredState(storage, key, decode, encode) {
  const [value, setValue] = useState(() => decode(storage.getItem(key)))
  useEffect(() => { storage.setItem(key, encode(value)) }, [value]) // eslint-disable-line react-hooks/exhaustive-deps
  return [value, setValue]
}

// useStoredFlag 布尔开关(存 '1'/'0');无记录时取 def。
export const useStoredFlag = (storage, key, def) =>
  useStoredState(storage, key, (s) => (s === null ? def : s === '1'), (v) => (v ? '1' : '0'))

// useStoredJSON 任意可 JSON 序列化的值;无记录或解析失败时经 sanitize(默认原样)兜底。
export const useStoredJSON = (storage, key, fallback, sanitize = (v) => v) =>
  useStoredState(
    storage, key,
    (s) => {
      try {
        const v = JSON.parse(s)
        if (v !== null && v !== undefined) return sanitize(v, fallback)
      } catch { /* ignore */ }
      return fallback
    },
    JSON.stringify,
  )
