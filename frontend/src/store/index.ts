import { configureStore } from '@reduxjs/toolkit'
import { TypedUseSelectorHook, useDispatch, useSelector } from 'react-redux'
import authReducer from './authSlice'
import tabsReducer, { saveTabsState } from './tabsSlice'

export const store = configureStore({
  reducer: {
    auth: authReducer,
    tabs: tabsReducer,
  },
})

let prevTabs = store.getState().tabs
store.subscribe(() => {
  const next = store.getState().tabs
  if (next === prevTabs) return
  prevTabs = next
  saveTabsState(next)
})

export type RootState = ReturnType<typeof store.getState>
export type AppDispatch = typeof store.dispatch

export const useAppDispatch = () => useDispatch<AppDispatch>()
export const useAppSelector: TypedUseSelectorHook<RootState> = useSelector
