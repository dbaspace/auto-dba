import { configureStore } from '@reduxjs/toolkit'
import { describe, expect, it, vi } from 'vitest'
import authReducer from '../../store/authSlice'
import tabsReducer from '../../store/tabsSlice'
import type { AppDispatch } from '../../store'
import { goTicketListAndClose } from './goTicketList'

describe('goTicketListAndClose', () => {
  it('opens the ticket list and closes the submit tab', () => {
    const store = configureStore({
      reducer: { auth: authReducer, tabs: tabsReducer },
    })
    store.dispatch({
      type: 'tabs/openTab',
      payload: { path: '/ops/tickets/dml', title: 'DML工单', force: true },
    })
    const navigate = vi.fn()
    goTicketListAndClose(
      store.dispatch as AppDispatch,
      navigate as never,
      { pathname: '/ops/tickets/dml', search: '' },
      [],
    )
    const tabs = store.getState().tabs
    expect(navigate).toHaveBeenCalledWith('/ops/tickets')
    expect(tabs.tabs.some((t) => t.path === '/ops/tickets/dml')).toBe(false)
    expect(tabs.tabs.some((t) => t.path === '/ops/tickets' && t.title === '工单列表')).toBe(true)
    expect(tabs.activeKey).toBe('/ops/tickets')
  })
})
