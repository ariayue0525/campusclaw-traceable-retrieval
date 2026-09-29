import type { AskResult, AskTurn, ChunkOptions, ChunkVectorsResponse, Material, ReindexResult, SearchMode, SearchResult, User } from './types'

export class ApiError extends Error { constructor(public status:number, message:string){ super(message) } }

// 部署前缀：生产为 /week04，开发环境为空。所有后端调用都带上它，
// 页面上不出现写死的地址，也就不会绕过 nginx 的反向代理。
const BASE = import.meta.env.BASE_URL.replace(/\/$/, '')

async function request<T>(url:string, options?:RequestInit):Promise<T>{
  const response=await fetch(`${BASE}${url}`,{credentials:'same-origin',...options,headers:{...(options?.body instanceof FormData?{}:{'Content-Type':'application/json'}),...options?.headers}})
  if(!response.ok){let message=`请求失败（${response.status}）`;try{message=(await response.json()).error||message}catch{};throw new ApiError(response.status,message)}
  if(response.status===204)return undefined as T
  return response.json()
}
export const api={
  login:(username:string,password:string)=>request<{user:User}>('/api/login',{method:'POST',body:JSON.stringify({username,password})}),
  logout:()=>request<void>('/api/logout',{method:'POST'}),
  me:()=>request<{user:User}>('/api/me'),
  materials:(q='')=>request<{materials:Material[]}>(`/api/materials?q=${encodeURIComponent(q)}`),
  material:(id:number)=>request<{material:Material}>(`/api/materials/${id}`),
  upload:(file:File,title:string,options:ChunkOptions,onProgress:(value:number)=>void)=>new Promise<{material:Material}>((resolve,reject)=>{const form=new FormData();form.append('file',file);if(title)form.append('title',title);for(const [k,v] of Object.entries(options))if(v!==undefined&&v!=='')form.append(k,String(v));const xhr=new XMLHttpRequest();xhr.open('POST',`${BASE}/api/materials`);xhr.withCredentials=true;xhr.upload.onprogress=e=>{if(e.lengthComputable)onProgress(Math.round(e.loaded/e.total*100))};xhr.onload=()=>{let body:any={};try{body=JSON.parse(xhr.responseText)}catch{};if(xhr.status>=200&&xhr.status<300)resolve(body);else reject(new ApiError(xhr.status,body.error||`上传失败（${xhr.status}）`))};xhr.onerror=()=>reject(new ApiError(0,'网络连接失败'));xhr.send(form)}),

  // 检索与问答：请求体里不带班级编号，班级边界由服务端按会话判定。
  search:(q:string,mode:SearchMode,limit=10)=>request<SearchResult>('/api/search',{method:'POST',body:JSON.stringify({q,mode,limit})}),
  ask:(q:string,history:AskTurn[])=>request<AskResult>('/api/ask',{method:'POST',body:JSON.stringify({q,history})}),

  // 切片与向量的对照视图，以及按策略重建索引。
  chunkVectors:(id:number)=>request<ChunkVectorsResponse>(`/api/materials/${id}/vectors`),
  reindex:(id:number,options:ChunkOptions)=>request<ReindexResult>(`/api/materials/${id}/reindex`,{method:'POST',body:JSON.stringify(options)}),
  fileUrl:(id:number)=>`${BASE}/api/materials/${id}/file`
}
