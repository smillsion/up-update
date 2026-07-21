import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SubscriptionsView from './SubscriptionsView.vue'

const mocks=vi.hoisted(()=>({request:vi.fn()}))
vi.mock('../api',()=>({
  request:mocks.request,
  json:(method:string,body:unknown)=>({method,body:JSON.stringify(body)}),
}))

const settings=(configured:boolean,status:string)=>({
  bilibili:{configured,autoRefresh:false,status,name:configured?'tester':'',lastValidated:null,error:''},
  bark:{configured:false,server:'https://api.day.app',level:'active',sound:''},
})

function mountView(){return mount(SubscriptionsView,{global:{stubs:{RouterLink:{template:'<a><slot/></a>'}}}})}

describe('SubscriptionsView',()=>{
  afterEach(()=>{mocks.request.mockReset();vi.useRealTimers()})

  it('disables subscription actions when the Bilibili cookie is unavailable',async()=>{
    mocks.request.mockImplementation((path:string)=>Promise.resolve(path==='/subscriptions'?[]:settings(false,'missing')))
    const wrapper=mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('订阅功能需要有效的 B 站 Cookie')
    expect(wrapper.get('button[title="从关注导入"]').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('button').find(button=>button.text()==='添加')?.attributes('disabled')).toBeDefined()
  })

  it('imports selected accounts from the current following page',async()=>{
    mocks.request.mockImplementation((path:string,options?:RequestInit)=>{
      if(path==='/subscriptions')return Promise.resolve([])
      if(path==='/settings')return Promise.resolve(settings(true,'valid'))
      if(path==='/subscriptions/followings?page=1')return Promise.resolve({items:[{mid:'2',name:'New UP',avatar:'',subscribed:false}],page:1,pageSize:50,total:1,totalPages:1})
      if(path==='/subscriptions/import-followings'&&options?.method==='POST')return Promise.resolve({imported:1,skipped:0,initialized:1,pending:0})
      return Promise.reject(new Error(`unexpected request: ${path}`))
    })
    const wrapper=mountView()
    await flushPromises()
    await wrapper.get('button[title="从关注导入"]').trigger('click')
    await flushPromises()
    await wrapper.get('.following-row input').setValue(true)
    await wrapper.findAll('.modal-actions .primary').at(-1)!.trigger('click')
    await flushPromises()

    expect(mocks.request).toHaveBeenCalledWith('/subscriptions/import-followings',expect.objectContaining({method:'POST',body:JSON.stringify({page:1,mids:['2']})}))
    expect(wrapper.text()).toContain('已导入 1 个订阅')
  })

  it('shows initialization state and refreshes it in the background',async()=>{
    vi.useFakeTimers()
    let subscriptionRequests=0
    const base={id:2,enabled:true,mid:'2',name:'New UP',avatar:'',latestBvid:'',latestTitle:'',subscribedAt:1,lastPolledAt:null,error:''}
    mocks.request.mockImplementation((path:string)=>{
      if(path==='/settings')return Promise.resolve(settings(true,'valid'))
      if(path==='/subscriptions'){
        subscriptionRequests++
        return Promise.resolve([{...base,...(subscriptionRequests>1?{latestBvid:'BV2',latestTitle:'Latest video',lastPolledAt:2}:{})}])
      }
      return Promise.reject(new Error(`unexpected request: ${path}`))
    })
    const wrapper=mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('正在获取最新投稿')

    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(subscriptionRequests).toBe(2)
    expect(wrapper.text()).toContain('Latest video')
    wrapper.unmount()
  })
})
