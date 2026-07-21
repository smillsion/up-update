import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SettingsView from './SettingsView.vue'

const mocks=vi.hoisted(()=>({request:vi.fn(),toCanvas:vi.fn().mockResolvedValue(undefined)}))
vi.mock('../api',()=>(
  {request:mocks.request,json:(method:string,body:unknown)=>({method,body:JSON.stringify(body)})}
))
vi.mock('qrcode',()=>({toCanvas:mocks.toCanvas}))

const settings=(autoRefresh=false)=>({
  bilibili:{configured:autoRefresh,autoRefresh,status:autoRefresh?'valid':'missing',name:autoRefresh?'tester':'',lastValidated:null,error:''},
  bark:{configured:false,server:'https://api.day.app',level:'active',sound:''},
})

function mountView(){return mount(SettingsView,{global:{stubs:{RouterLink:{template:'<a><slot/></a>'}}}})}

describe('SettingsView',()=>{
  afterEach(()=>{mocks.request.mockReset();mocks.toCanvas.mockClear();vi.useRealTimers()})

  it('completes QR login and enables automatic refresh',async()=>{
    vi.useFakeTimers()
    let configured=false,polls=0
    mocks.request.mockImplementation((path:string,options?:RequestInit)=>{
      if(path==='/settings')return Promise.resolve(settings(configured))
      if(path==='/settings/bilibili/qrcode'&&options?.method==='POST')return Promise.resolve({sessionId:'qr-1',qrUrl:'https://example/qr',expiresAt:9999999999})
      if(path==='/settings/bilibili/qrcode/qr-1/poll'&&options?.method==='POST'){
        polls++;if(polls===1)return Promise.resolve({status:'scanned',message:'已扫码，请在手机上确认登录'})
        configured=true;return Promise.resolve({status:'success',message:'扫码登录成功',name:'tester'})
      }
      return Promise.reject(new Error(`unexpected request: ${path}`))
    })
    const wrapper=mountView();await flushPromises()
    await wrapper.get('button.primary').trigger('click');await flushPromises()

    expect(mocks.toCanvas).toHaveBeenCalled()
    expect(wrapper.text()).toContain('请使用哔哩哔哩客户端扫码')
    await vi.advanceTimersByTimeAsync(1500);await flushPromises()
    expect(wrapper.text()).toContain('已扫码，请在手机上确认登录')
    await vi.advanceTimersByTimeAsync(1500);await flushPromises()
    expect(wrapper.text()).toContain('自动续期')
    expect(wrapper.text()).toContain('已登录 B 站账号 tester')
    wrapper.unmount()
  })
})
