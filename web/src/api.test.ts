import { afterEach, describe, expect, it, vi } from 'vitest'
import { json, request, setCSRF } from './api'

describe('request',()=>{
  afterEach(()=>vi.unstubAllGlobals())
  it('adds csrf token to mutating requests',async()=>{
    const fetchMock=vi.fn().mockResolvedValue(new Response(JSON.stringify({ok:true}),{status:200,headers:{'Content-Type':'application/json'}}))
    vi.stubGlobal('fetch',fetchMock);setCSRF('csrf-test')
    await request('/test',json('POST',{value:1}))
    const options=fetchMock.mock.calls[0][1] as RequestInit
    expect(new Headers(options.headers).get('X-CSRF-Token')).toBe('csrf-test')
  })
  it('raises structured API errors',async()=>{
    vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({error:'failed',code:'bad'}),{status:400})))
    await expect(request('/test')).rejects.toEqual(expect.objectContaining({message:'failed',code:'bad',status:400}))
  })
})
