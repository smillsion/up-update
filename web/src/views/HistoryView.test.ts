import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import HistoryView from './HistoryView.vue'

const mocks=vi.hoisted(()=>({request:vi.fn(),replace:vi.fn()}))
vi.mock('../api',()=>({request:mocks.request}))
vi.mock('vue-router',()=>({
  useRoute:()=>({query:{}}),
  useRouter:()=>({replace:mocks.replace}),
}))

const delivery=(id:number)=>({id,status:'sent',attempts:1,error:'',createdAt:id,sentAt:id,bvid:`BV${id}`,videoTitle:`Video ${id}`,videoUrl:`https://example/${id}`,creatorName:'UP',creatorAvatar:''})

describe('HistoryView',()=>{
  afterEach(()=>{mocks.request.mockReset();mocks.replace.mockReset()})

  it('requests and displays server-side pages',async()=>{
    mocks.request.mockImplementation((path:string)=>{
      const page=path.endsWith('page=2')?2:1
      return Promise.resolve({items:[delivery(page)],page,pageSize:20,total:41,totalPages:3})
    })
    const wrapper=mount(HistoryView)
    await flushPromises()
    const secondPage=wrapper.findAll('.page-number').find(button=>button.text()==='2')
    expect(secondPage).toBeDefined()
    await secondPage!.trigger('click')
    await flushPromises()

    expect(mocks.request).toHaveBeenCalledWith('/deliveries?page=2')
    expect(mocks.replace).toHaveBeenLastCalledWith({query:{page:'2'}})
    expect(wrapper.text()).toContain('共 41 条')
  })
})
