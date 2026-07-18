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

// InlineIcon 渲染文字前的小图标(系别/六维/血脉等);无路径或加载失败则不占位(留文字)。
export function InlineIcon({ src, className = 'inline-ic', alt = '' }) {
  const [bad, onError] = useImgFallback(src)
  if (!src || bad) return null
  return <img className={className} src={imgURL(src)} alt={alt} loading="lazy" onError={onError} />
}

// StatIcon 按六维键(hp/attack/…)从 IconsContext 取对应属性小图。
export function StatIcon({ statKey, className = 'stat-ic' }) {
  const icons = React.useContext(IconsContext)
  return <InlineIcon src={icons.stat && icons.stat[statKey]} className={className} alt="" />
}

// ImgAvatar 按图片相对路径渲染一个头像(进化链等无 pet 对象处用);缺图回退 emoji。
export function ImgAvatar({ src, alt = '', className = 'pet-avatar' }) {
  const [bad, onError] = useImgFallback(src)
  if (src && !bad) {
    return <img className={className} src={imgURL(src)} alt={alt} loading="lazy" onError={onError} />
  }
  return <div className={className}>🐾</div>
}
