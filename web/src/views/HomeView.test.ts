import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { session } from '../state'
import HomeView from './HomeView.vue'

const RouterLink={props:['to'],template:'<a :data-to="to"><slot/></a>'}
function mountView(){return mount(HomeView,{global:{stubs:{RouterLink}}})}

describe('HomeView',()=>{
  afterEach(()=>{session.user=null})

  it('sends visitors to login',()=>{
    const wrapper=mountView()
    const action=wrapper.findAll('[data-to="/login"]').find(item=>item.text()==='登录系统')
    expect(action?.exists()).toBe(true)
    expect(wrapper.text()).toContain('无需开启 B 站 App 的消息通知')
  })

  it('sends authenticated users back to the console',()=>{
    session.user={id:1,username:'demo',displayName:'演示用户',role:'user',forcePasswordChange:false,csrfToken:'csrf'}
    const wrapper=mountView()
    const action=wrapper.findAll('[data-to="/subscriptions"]').find(item=>item.text()==='进入控制台')
    expect(action?.exists()).toBe(true)
  })
})
