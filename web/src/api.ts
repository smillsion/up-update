import type { User } from './types'

export class ApiError extends Error { constructor(message:string, public code='', public status=0){super(message)} }
let csrfToken = ''

export async function request<T>(path:string, options:RequestInit={}, redirectUnauthorized=true):Promise<T>{
  const method=(options.method||'GET').toUpperCase()
  const headers=new Headers(options.headers)
  if(options.body) headers.set('Content-Type','application/json')
  if(!['GET','HEAD','OPTIONS'].includes(method)&&csrfToken) headers.set('X-CSRF-Token',csrfToken)
  const response=await fetch('/api'+path,{...options,headers,credentials:'same-origin'})
  if(response.status===204) return undefined as T
  const data=await response.json().catch(()=>({error:'服务返回了无法解析的内容'}))
  if(!response.ok){
    if(response.status===401&&redirectUnauthorized&&window.location.pathname!='/login') window.location.assign('/login')
    throw new ApiError(data.error||'请求失败',data.code||'',response.status)
  }
  return data as T
}

export function setCSRF(value:string){csrfToken=value}
export function json(method:string, body:unknown):RequestInit{return{method,body:JSON.stringify(body)}}

export const auth={
  async login(username:string,password:string){const user=await request<User>('/auth/login',json('POST',{username,password}));setCSRF(user.csrfToken);return user},
  async me(){const user=await request<User>('/auth/me',{},false);setCSRF(user.csrfToken);return user},
  logout(){return request<void>('/auth/logout',{method:'POST'})}
}
