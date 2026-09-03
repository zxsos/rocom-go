import React, { Suspense, lazy } from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter, Routes, Route, Navigate } from 'react-router-dom'
import App from './App'
import PetList from './pages/pet-list/PetList'
// 其余页面路由级懒加载：首屏只加载落地页(宠物列表)与 App 壳，
// 地图引擎等重量级依赖跟随各自页面分包，不进首屏 bundle。
const Events = lazy(() => import('./pages/events/Events'))
const PetDetail = lazy(() => import('./pages/pet-detail/PetDetail'))
const Debug = lazy(() => import('./pages/debug/Debug'))
const MapPage = lazy(() => import('./pages/map/MapPage'))
const EggList = lazy(() => import('./pages/eggs/EggList'))
const Merchant = lazy(() => import('./pages/merchant/Merchant'))
const Flowers = lazy(() => import('./pages/flowers/Flowers'))
const Trial = lazy(() => import('./pages/trial/Trial'))
const HandbookGlasses = lazy(() => import('./pages/handbook/HandbookGlasses'))
const Leaderboard = lazy(() => import('./pages/leaderboard/Leaderboard'))
const Admin = lazy(() => import('./pages/admin/Admin'))
// 样式按「基础 → 壳 → 共用面板/部件 → 各页」顺序引入(同名选择器的层叠顺序有意义)。
import './styles/base.css'
import './styles/dropdown.css'
import './styles/shell.css'
import './styles/panel.css'
import './styles/pet.css'
import './styles/list.css'
import './styles/events.css'
import './styles/eggs.css'
import './styles/merchant.css'
import './styles/detail.css'
import './styles/map.css'
import './styles/debug.css'
// 各页面的入场过渡编排,集中一处 —— 见文件头的说明:
// 节奏差异要对着看才调得出来,分开写会各自发明一套缓动。
import './styles/motion.css'
import './styles/flowers.css'
import './styles/trial.css'
import './styles/handbook.css'
import './styles/leaderboard.css'
import './styles/admin.css'
import './styles/pin.css'
import './styles/rules.css'

// 路由懒加载的兜底占位(P5 将升级为与页面布局同构的骨架屏)。
function PageLoading() {
  return (
    <div className="page-loading" role="status" aria-label="页面加载中">
      <span className="spinner" aria-hidden="true" />
    </div>
  )
}

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <HashRouter>
      <Suspense fallback={<PageLoading />}>
        <Routes>
          <Route element={<App />}>
            <Route index element={<Navigate to="/pets" replace />} />
            <Route path="pets" element={<PetList />} />
            <Route path="pets/:gid" element={<PetDetail />} />
            <Route path="events" element={<Events />} />
            <Route path="eggs" element={<EggList />} />
            <Route path="merchant" element={<Merchant />} />
            <Route path="map" element={<MapPage />} />
            <Route path="flowers" element={<Flowers />} />
            <Route path="trial" element={<Trial />} />
            <Route path="handbook" element={<HandbookGlasses />} />
            <Route path="leaderboard" element={<Leaderboard />} />
            {/* 调试页:导航不显示,需手动输入 #/debug */}
            <Route path="debug" element={<Debug />} />
            {/* 隐式管理面板:导航不显示,需手动输入 #/admin */}
            <Route path="admin" element={<Admin />} />
          </Route>
        </Routes>
      </Suspense>
    </HashRouter>
  </React.StrictMode>
)
