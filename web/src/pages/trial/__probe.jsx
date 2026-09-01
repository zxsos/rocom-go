// 一次性渲染探针(不进构建:文件名以 __ 开头,且只被验证脚本 import)。
// 用途:在 jsdom 里把 ElementWheel 渲染成真实 DOM,校验结构齐全与几何正确。
// 删掉它不影响任何线上功能。
import { renderToStaticMarkup } from 'react-dom/server'
import ElementWheel from './ElementWheel'

export function probe(slots, theme) {
  return renderToStaticMarkup(
    <ElementWheel slots={slots} theme={theme} />,
  )
}
