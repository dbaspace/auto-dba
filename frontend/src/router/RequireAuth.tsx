import React from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { collectPaths, pathAllowed, firstAllowed } from '../constants/routes'

function allowedPaths(): string[] {
  try {
    const u = JSON.parse(localStorage.getItem('user') || '{}')
    return collectPaths(u.menus || [])
  } catch {
    return []
  }
}

export default function RequireAuth({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const token = localStorage.getItem('token')
  const path = location.pathname

  if (!token) {
    return <Navigate to={`/login?redirect=${encodeURIComponent(location.pathname + location.search)}`} replace />
  }
  if (path === '/') {
    return <Navigate to={firstAllowed(allowedPaths())} replace />
  }
  const allowed = allowedPaths()
  if (allowed.length && !pathAllowed(path, allowed)) {
    return <Navigate to="/403" replace />
  }
  return <>{children}</>
}
