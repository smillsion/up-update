export interface User { id:number; username:string; displayName:string; role:'admin'|'user'; forcePasswordChange:boolean; csrfToken:string }
export interface Settings {
  bilibili:{configured:boolean;status:string;name:string;lastValidated:number|null;error:string}
  bark:{configured:boolean;server:string;level:string;sound:string}
}
export interface Subscription {id:number;enabled:boolean;mid:string;name:string;avatar:string;latestBvid:string;latestTitle:string;subscribedAt:number;lastPolledAt:number|null;error:string}
export interface Delivery {id:number;status:'pending'|'sent'|'failed';attempts:number;error:string;createdAt:number;sentAt:number|null;bvid:string;videoTitle:string;videoUrl:string;creatorName:string;creatorAvatar:string}
export interface Following {mid:string;name:string;avatar:string;subscribed:boolean}
export interface PageResult<T> {items:T[];page:number;pageSize:number;total:number;totalPages:number}
export interface FollowingImportResult {imported:number;skipped:number}
