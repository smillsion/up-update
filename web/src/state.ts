import { reactive } from 'vue'
import { auth, ApiError } from './api'
import type { User } from './types'

export const session=reactive<{user:User|null;ready:boolean}>({user:null,ready:false})
export async function restoreSession(){try{session.user=await auth.me()}catch(error){if(!(error instanceof ApiError&&error.status===401))console.error(error);session.user=null}finally{session.ready=true}}
