import { createRouter, createWebHistory } from 'vue-router'
import { session } from './state'
import LoginView from './views/LoginView.vue'
import SubscriptionsView from './views/SubscriptionsView.vue'
import HistoryView from './views/HistoryView.vue'
import SettingsView from './views/SettingsView.vue'
import AdminView from './views/AdminView.vue'
import PasswordView from './views/PasswordView.vue'

const router=createRouter({history:createWebHistory(),routes:[
  {path:'/login',component:LoginView,meta:{public:true}},
  {path:'/',redirect:'/subscriptions'},
  {path:'/subscriptions',component:SubscriptionsView},
  {path:'/history',component:HistoryView},
  {path:'/settings',component:SettingsView},
  {path:'/password',component:PasswordView},
  {path:'/admin',component:AdminView,meta:{admin:true}},
  {path:'/:pathMatch(.*)*',redirect:'/subscriptions'}
]})
router.beforeEach((to)=>{
  if(to.meta.public){if(session.user)return '/subscriptions';return true}
  if(!session.user)return '/login'
  if(session.user.forcePasswordChange&&to.path!='/password')return '/password'
  if(to.meta.admin&&session.user.role!=='admin')return '/subscriptions'
  return true
})
export default router
