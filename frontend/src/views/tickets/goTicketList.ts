import type { Location, NavigateFunction } from 'react-router-dom'
import type { AppDispatch } from '../../store'
import type { MenuNode } from '../../store/authSlice'
import { closeTab, openTab, tabKey } from '../../store/tabsSlice'

/** 打开工单列表并关闭当前提交页。 */
export function goTicketListAndClose(
  dispatch: AppDispatch,
  navigate: NavigateFunction,
  location: Pick<Location, 'pathname' | 'search'>,
  menus?: MenuNode[],
) {
  const curKey = tabKey(location.pathname, location.search)
  dispatch(openTab({ path: '/ops/tickets', menus, title: '工单列表' }))
  dispatch(closeTab(curKey))
  navigate('/ops/tickets')
}
