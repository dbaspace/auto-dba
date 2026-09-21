import { createSlice, PayloadAction } from '@reduxjs/toolkit'

export interface MenuNode {
  id?: string
  title: string
  path?: string
  icon?: string
  children?: MenuNode[]
}

export interface CurrentUser {
  id?: string
  username?: string
  display_name?: string
  avatar?: string
  menus?: MenuNode[]
  [k: string]: unknown
}

function readUser(): CurrentUser {
  try {
    return JSON.parse(localStorage.getItem('user') || '{}')
  } catch {
    return {}
  }
}

interface AuthState {
  user: CurrentUser
  menus: MenuNode[]
}

const initial: AuthState = {
  user: readUser(),
  menus: readUser().menus || [],
}

const authSlice = createSlice({
  name: 'auth',
  initialState: initial,
  reducers: {
    setUser(state, action: PayloadAction<CurrentUser>) {
      state.user = action.payload || {}
      state.menus = action.payload?.menus || []
      localStorage.setItem('user', JSON.stringify(state.user))
    },
    patchUser(state, action: PayloadAction<Partial<CurrentUser>>) {
      state.user = { ...state.user, ...(action.payload || {}) }
      if (action.payload?.menus) state.menus = action.payload.menus
      localStorage.setItem('user', JSON.stringify(state.user))
    },
    clearAuth(state) {
      state.user = {}
      state.menus = []
      localStorage.removeItem('user')
      localStorage.removeItem('token')
    },
  },
})

export const { setUser, patchUser, clearAuth } = authSlice.actions
export default authSlice.reducer
