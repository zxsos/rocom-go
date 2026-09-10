// 仓库示意图的容器拼装:大世界队伍排在所有盒子最前,其后各盒子。
//
// 原先这段内联在 PetList 里,培育页的「定位」弹窗要复用同一张图 —— 格位数与列数必须与仓库
// 完全一致,玩家才能照着数出「第几盒第几排第几格」,故下沉成纯函数:两页各拉各的数据
// (getTeams / getBoxes),但拼出来的图必须一模一样。
//
// 文件名**不要**用 boxmap.js:同目录下的组件是 BoxMap.jsx,两者只差大小写,而 Windows
// 文件系统不区分大小写 —— `import BoxMap from './BoxMap'` 会命中 boxmap.js(vite 按
// .js 优先于 .jsx 探测),报「default is not exported」,且只在构建时才暴露。
export function buildContainers(teams, boxes) {
  // 原始 18 格为队序(team*6+pos);转置为「行=位置、列=队伍」的显示序(pos*3+team)
  const raw = (teams && teams.slots && teams.slots.length) ? teams.slots : new Array(18).fill(0)
  const teamDisplay = []
  for (let pos = 0; pos < 6; pos++) for (let t = 0; t < 3; t++) teamDisplay.push(raw[t * 6 + pos])
  const list = [{ type: 'team', name: '大世界队伍', cols: 3, slots: teamDisplay, heads: (teams && teams.heads) || {} }]
  for (const b of boxes || []) {
    list.push({ type: 'box', id: b.id, name: b.name || ('盒' + b.id), cols: 6, slots: b.slots, heads: b.heads || {} })
  }
  return list
}

// boxIndexOf 按盒 id 找容器下标(找不到返回 -1)。宠物页要靠它让示意图跟随筛选里的盒子,
// 定位弹窗要靠它把那格所在的盒切到眼前 —— 同一套下标口径,不能各算各的。
export function boxIndexOf(containers, id) {
  return containers.findIndex((c) => c.type === 'box' && c.id === id)
}
