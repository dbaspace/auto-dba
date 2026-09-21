import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus, { ElDialog, ElDrawer } from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import App from './App.vue'
import router from './router'
import { installOverlayGuard } from './utils/overlay'
import './styles.css'

if (ElDialog.props?.appendToBody) ElDialog.props.appendToBody.default = true
if (ElDialog.props?.modal) ElDialog.props.modal.default = false
if (ElDialog.props?.lockScroll) ElDialog.props.lockScroll.default = false
if (ElDialog.props?.alignCenter) ElDialog.props.alignCenter.default = true
if (ElDrawer.props?.appendToBody) ElDrawer.props.appendToBody.default = true
if (ElDrawer.props?.lockScroll) ElDrawer.props.lockScroll.default = false

createApp(App).use(createPinia()).use(router).use(ElementPlus, { locale: zhCn }).mount('#app')
installOverlayGuard()
