import React from 'react'
import { IconsContext } from '../context'

// imgURL 把后端下发的图片相对路径拼成静态资源 URL。
export const imgURL = (path) => '/img/' + path

// useImgFallback 图片加载失败的通用回退:返回 [是否失败, onError];src 变化时重置。
export function useImgFallback(src) {
  const [bad, setBad] = React.useState(false)
  React.useEffect(() => setBad(false), [src])
  return [bad, () => setBad(true)]
}

// useImgReady 图片「已就位」标记:返回 [是否就位, onLoad, 挂载回调]。
//
// 占位底色来自 .img-ph(见 base.css),挂在 <img> 自己身上而非另插元素 ——
// 图片尺寸本来就由各自的类定好,铺背景就是尺寸正确的灰块,加载完摘类即可,
// 没有多余 DOM、也不会出现「占位与图片并存」导致的跳位。
//
// ref 回调里补查 el.complete 是**必须的**:图片命中缓存时,浏览器可能在 React
// 挂上 onLoad 之前就已完成加载,那时 onLoad 永远不会触发 —— 只靠 onLoad 会
// 让缓存命中的图永远停在占位态(刷新一次才正常,极易被误判为偶发 bug)。
export function useImgReady(src) {
  const [ready, setReady] = React.useState(false)
  React.useEffect(() => setReady(false), [src])
  const onLoad = React.useCallback(() => setReady(true), [])
  const onRef = React.useCallback((el) => { if (el && el.complete) setReady(true) }, [])
  return [ready, onLoad, onRef]
}

// InlineIcon 渲染文字前的小图标(系别/六维/血脉等);无路径或加载失败则不占位(留文字)。
export function InlineIcon({ src, className = 'inline-ic', alt = '' }) {
  const [bad, onError] = useImgFallback(src)
  const [ready, onLoad, onRef] = useImgReady(src)
  if (!src || bad) return null
  return (
    <img
      ref={onRef}
      className={className + (ready ? '' : ' img-ph')}
      src={imgURL(src)} alt={alt} loading="lazy"
      onLoad={onLoad} onError={onError}
    />
  )
}

// StatIcon 按六维键(hp/attack/…)从 IconsContext 取对应属性小图。
export function StatIcon({ statKey, className = 'stat-ic' }) {
  const icons = React.useContext(IconsContext)
  return <InlineIcon src={icons.stat && icons.stat[statKey]} className={className} alt="" />
}

// ImgAvatar 按图片相对路径渲染一个头像(进化链等无 pet 对象处用);缺图回退 emoji。
export function ImgAvatar({ src, alt = '', className = 'pet-avatar' }) {
  const [bad, onError] = useImgFallback(src)
  const [ready, onLoad, onRef] = useImgReady(src)
  if (src && !bad) {
    return (
      <img
        ref={onRef}
        className={className + (ready ? '' : ' img-ph')}
        src={imgURL(src)} alt={alt} loading="lazy"
        onLoad={onLoad} onError={onError}
      />
    )
  }
  return <div className={className}>🐾</div>
}
