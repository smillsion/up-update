import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AdminView from './AdminView.vue'

const mocks=vi.hoisted(()=>({request:vi.fn()}))
vi.mock('../api',()=>({request:mocks.request,json:(method:string,body:unknown)=>({method,body:JSON.stringify(body)})}))

const schedule={timezone:'Asia/Shanghai',sleep:{start:'00:00',end:'08:00',intervalMinutes:120},work:{windows:[{start:'09:00',end:'12:00'}],intervalMinutes:15},free:{intervalMinutes:5}}
const system={pollIntervalSeconds:300,pollSchedule:schedule,currentPeriod:'free',currentIntervalMinutes:5,nextTransitionAt:9999999999,activeCreators:0,activeUsers:2,pendingDeliveries:0}

describe('AdminView',()=>{
  afterEach(()=>mocks.request.mockReset())

  it('requires an exact username before permanently deleting a user',async()=>{
    let deleted=false
    mocks.request.mockImplementation((path:string,options?:RequestInit)=>{
      if(path==='/admin/users'&&!options)return Promise.resolve(deleted?[]:[{id:2,username:'member',displayName:'Member',role:'user',enabled:true,forcePasswordChange:false,createdAt:1,bilibiliStatus:'valid',barkConfigured:true,subscriptions:3}])
      if(path==='/admin/system')return Promise.resolve(system)
      if(path==='/admin/users/2'&&options?.method==='DELETE'){deleted=true;return Promise.resolve()}
      return Promise.reject(new Error(`unexpected request: ${path}`))
    })
    const wrapper=mount(AdminView);await flushPromises()
    await wrapper.get('button[title="永久删除用户"]').trigger('click')
    const dialog=wrapper.get('[role="dialog"]')
    const submit=dialog.get('button.destructive-action')
    expect(submit.attributes('disabled')).toBeDefined()
    await dialog.get('input').setValue('member')
    expect(submit.attributes('disabled')).toBeUndefined()
    await dialog.get('form').trigger('submit');await flushPromises()

    expect(mocks.request).toHaveBeenCalledWith('/admin/users/2',{method:'DELETE',body:JSON.stringify({confirmUsername:'member'})})
    expect(wrapper.text()).toContain('用户及其数据已永久删除')
  })
})
