import React from 'react'
import { createRoot } from 'react-dom/client'
import { HashRouter, Routes, Route, Navigate } from 'react-router-dom'
import App from './App'
import PetList from './pages/pet-list/PetList'
import Events from './pages/events/Events'
import PetDetail from './pages/PetDetail'
import Debug from './pages/Debug'
import MapPage from './pages/map/MapPage'
// 样式按「基础 → 壳 → 共用面板/部件 → 各页」顺序引入(同名选择器的层叠顺序有意义)。
import './styles/base.css'
import './styles/shell.css'
import './styles/panel.css'
import './styles/pet.css'
import './styles/list.css'
import './styles/events.css'
import './styles/detail.css'
import './styles/map.css'
import './styles/debug.css'

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <HashRouter>
      <Routes>
        <Route element={<App />}>
          <Route index element={<Navigate to="/pets" replace />} />
          <Route path="pets" element={<PetList />} />
          <Route path="pets/:gid" element={<PetDetail />} />
          <Route path="events" element={<Events />} />
          <Route path="map" element={<MapPage />} />
          <Route path="debug" element={<Debug />} />
        </Route>
      </Routes>
    </HashRouter>
  </React.StrictMode>
)
