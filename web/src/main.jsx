import React, { useEffect, useState, useRef, useMemo } from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter, Routes, Route, Navigate } from 'react-router-dom'
import App from './App'
import PetList from './pages/pet-list/PetList'
import Events from './pages/events/Events'
import PetDetail from './pages/PetDetail'
import Debug from './pages/Debug'
import MapPage from './pages/map/MapPage'
import EggList from './pages/eggs/EggList'
import Merchant from './pages/merchant/Merchant'
import Flowers from './pages/Flowers'
import HandbookGlasses from './pages/HandbookGlasses'
import Leaderboard from './pages/leaderboard/Leaderboard'
import Admin from './pages/Admin'
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
import './styles/flowers.css'
import './styles/handbook.css'
import './styles/leaderboard.css'
import './styles/admin.css'
import './styles/pin.css'

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <HashRouter>
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
          <Route path="handbook" element={<HandbookGlasses />} />
          <Route path="leaderboard" element={<Leaderboard />} />
          {/* 调试页:导航不显示,需手动输入 #/debug */}
          <Route path="debug" element={<Debug />} />
          {/* 隐式管理面板:导航不显示,需手动输入 #/admin */}
          <Route path="admin" element={<Admin />} />
        </Route>
      </Routes>
    </HashRouter>
  </React.StrictMode>
)
