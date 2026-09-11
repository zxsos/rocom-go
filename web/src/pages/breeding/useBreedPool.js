import { useCallback, useContext, useMemo } from 'react'
import { getBreedingPool } from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'

// useBreedPool 按**品种**(evo + species)取一次候选池:三类候选(种母 / 种公 / 子代)。
//
// 为什么是共享 hook 而不是各自一份:培育线详情(补录面板)与「新建培育线」表单都要这份候选,
// 两处必须**同一套口径** —— 各写一份迟早分叉,而分叉的表现是「详情里能选到的候选,建线时
// 选不到」,玩家只会以为库里的那只不见了。原先它长在 LineDetail 里,现在提到这里。
//
// 一个查询而不是两次 200 条分页:分页的天花板(store.clampPageSize)正好卡在候选池的要害上 ——
// 漏掉一页就等于「库里还有更合适的个体,玩家却无从知道」。三个队列的范围规则(同品种 ♀、
// 与母本同蛋组的 ♂、同品种)都只跟品种有关,后端一并算完,前端只做关键词过滤。
//
// 按品种缓存:同一条线内多次展开 / 收起不重拉;改目标也**不重拉** —— 候选池与目标无关
// (见 api.js 的 getBreedingPool)。两个字段都要进依赖:无链形态的 evo 是 0,仅凭它区分不出品种。
//
// 品种为空时**也要拉**:那就是「还没定品种」,后端这时给全库的雌雄 —— 建线表单允许先挑种母 /
// 种公(再由种母把品种带出来,蛋随母本)。不拉的话「先选种母」这一步在界面上表现为空下拉,
// 什么都不报,玩家只会以为库里没有可选的。
export default function useBreedPool(ref) {
  const account = useContext(AccountContext)
  const evo = (ref && ref.evo) || 0
  const species = (ref && ref.species) || ''

  const { data, loading } = useAsyncData(
    useCallback(() => getBreedingPool(evo, species), [evo, species]),
    { fallback: null, reloadKey: account },
  )

  return useMemo(() => ({
    loading: !!loading,
    // ready 回答的是「**这个品种的候选到底有没有拿到**」,与 loading 分开:调用方要据它
    // 判断「已选的那位不在候选里」到底是「配不上」还是「还没拉到那个品种的池子」——
    // 混起来会出现两种都很难看的错(把没拉到的清掉 / 把配不上的留着)。
    ready: !!data,
    mothers: sortPool(data && data.mothers),
    fathers: sortPool(data && data.fathers),
    kids: sortPool(data && data.kids),
  }), [data, loading])
}

// sortPool 候选按嗓音绝对值降序:极端个体排在前面,先看到的多半是玩家真正想要的。
// 复制一份再排:后端给的数组还归 useAsyncData 持有,原地排会把它也改掉。
const sortPool = (list) => (list || []).slice().sort(byVoiceDesc)

const byVoiceDesc = (a, b) => Math.abs(b.voice || 0) - Math.abs(a.voice || 0)
