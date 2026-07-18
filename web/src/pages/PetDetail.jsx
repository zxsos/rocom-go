import React from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { PetDetailModal } from '../components/PetDetailModal'

// 路由页:直接访问 /pets/:gid 或从其他页跳转时,以弹窗形式呈现,关闭即返回上一页。
export default function PetDetail() {
  const { gid } = useParams()
  const nav = useNavigate()
  return <PetDetailModal gid={gid} onClose={() => nav(-1)} />
}
