import type { AskResult, AskTurn, ChunkOptions, ChunkVectorsResponse, Material, ReindexResult, SearchMode, SearchResult, User } from './types'

export class ApiError extends Error { constructor(public status:number, message:string){ super(message) } }

// 部署前缀：生产为 /week04，开发环境为空。所有后端调用都带上它，
// 页面上不出现写死的地址，也就不会绕过 nginx 的反向代理。
const BASE = import.meta.env.BASE_URL.replace(/\/$/, '')

// ---- JWT Bearer 凭证管理 ----
// access_token（JWT，短时效）与 refresh_token（长时效、可吊销）保存在 localStorage。
// 每个请求经 request() 统一附加 Authorization 头；401 时先用 refresh 静默续期一次再重放。
const ACCESS_KEY = 'campusclaw.access'
const REFRESH_KEY = 'campusclaw.refresh'

export const tokens = {
  access: () => localStorage.getItem(ACCESS_KEY) || '',
  refresh: () => localStorage.getItem(REFRESH_KEY) || '',
  clear() { localStorage.removeItem(ACCESS_KEY); localStorage.removeItem(REFRESH_KEY) },
  store(access: string, refresh?: string) {
    if (access) localStorage.setItem(ACCESS_KEY, access)
    if (refresh) localStorage.setItem(REFRESH_KEY, refresh)
  }
}

type TokenPair = { token_type:string; access_token:string; expires_in:number; refresh_token:string; user:User }

function authHeaders(): Record<string,string> {
  const access = tokens.access()
  return access ? { Authorization: `Bearer ${access}` } : {}
}

let refreshing: Promise<boolean> | null = null

// refreshTokens 用 refresh_token 换新 token 对（服务端轮换：旧串立即作废）。
// 并发请求共享同一次续期，避免多个 401 同时打出多个 refresh。
function refreshTokens(): Promise<boolean> {
  if (!refreshing) {
    refreshing = (async () => {
      const refresh = tokens.refresh()
      if (!refresh) return false
      try {
        const res = await fetch(`${BASE}/api/token/refresh`, { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({ refresh_token:refresh }) })
        if (!res.ok) { tokens.clear(); return false }
        const body:TokenPair = await res.json()
        tokens.store(body.access_token, body.refresh_token)
        return true
      } catch { return false }
      finally { refreshing = null }
    })()
  }
  return refreshing
}

async function request<T>(url:string, options:RequestInit & { skipAuthRetry?:boolean } = {}):Promise<T>{
  const { skipAuthRetry, ...init } = options
  const response = await fetch(`${BASE}${url}`, {
    ...init,
    headers:{ ...(init.body instanceof FormData ? {} : { 'Content-Type':'application/json' }), ...authHeaders(), ...init.headers }
  })
  // 登录与续期自身的 401 不再重试；其余请求 401 时先续期一次并重放。
  if (response.status === 401 && !skipAuthRetry && !url.startsWith('/api/login') && !url.startsWith('/api/token/refresh')) {
    if (await refreshTokens()) return request<T>(url, { ...init, skipAuthRetry:true })
  }
  if(!response.ok){let message=`请求失败（${response.status}）`;try{message=(await response.json()).error||message}catch{};throw new ApiError(response.status,message)}
  if(response.status===204)return undefined as T
  return response.json()
}

export const api={
  // 登录成功后保存 token 对；后续请求都靠 Authorization 头携带 access_token。
  async login(username:string,password:string){ const body=await request<TokenPair>('/api/login',{method:'POST',body:JSON.stringify({username,password})}); tokens.store(body.access_token,body.refresh_token); return { user:body.user } },
  async logout(){ const refresh=tokens.refresh(); try{ await request<void>('/api/logout',{method:'POST',body:JSON.stringify({ refresh_token:refresh }), skipAuthRetry:true}) } finally{ tokens.clear() } },
  me:()=>request<{user:User}>('/api/me'),
  materials:(q='')=>request<{materials:Material[]}>(`/api/materials?q=${encodeURIComponent(q)}`),
  material:(id:number)=>request<{material:Material}>(`/api/materials/${id}`),
  upload:(file:File,title:string,options:ChunkOptions,onProgress:(value:number)=>void)=>new Promise<{material:Material}>((resolve,reject)=>{
    const send=()=>{ const form=new FormData();form.append('file',file);if(title)form.append('title',title);for(const [k,v] of Object.entries(options))if(v!==undefined&&v!=='')form.append(k,String(v));const xhr=new XMLHttpRequest();xhr.open('POST',`${BASE}/api/materials`);for(const [k,v] of Object.entries(authHeaders()))xhr.setRequestHeader(k,v);xhr.upload.onprogress=e=>{if(e.lengthComputable)onProgress(Math.round(e.loaded/e.total*100))};xhr.onload=()=>{let body:any={};try{body=JSON.parse(xhr.responseText)}catch{};if(xhr.status>=200&&xhr.status<300)resolve(body);else reject(new ApiError(xhr.status,body.error||`上传失败（${xhr.status}）`))};xhr.onerror=()=>reject(new ApiError(0,'网络连接失败'));xhr.send(form) }
    send()
  }),

  // 检索与问答：请求体里不带班级编号，班级边界由服务端按登录身份判定。
  search:(q:string,mode:SearchMode,limit=10)=>request<SearchResult>('/api/search',{method:'POST',body:JSON.stringify({q,mode,limit})}),
  ask:(q:string,history:AskTurn[])=>request<AskResult>('/api/ask',{method:'POST',body:JSON.stringify({q,history})}),

  // 切片与向量的对照视图，以及按策略重建索引。
  chunkVectors:(id:number)=>request<ChunkVectorsResponse>(`/api/materials/${id}/vectors`),
  reindex:(id:number,options:ChunkOptions)=>request<ReindexResult>(`/api/materials/${id}/reindex`,{method:'POST',body:JSON.stringify(options)}),

  // Bearer 方案下 <a href> 无法携带 Authorization 头，下载改为 fetch + blob。
  async downloadFile(id:number,fallbackName:string){
    let res = await fetch(`${BASE}/api/materials/${id}/file`,{ headers:authHeaders() })
    if (res.status === 401 && await refreshTokens()) res = await fetch(`${BASE}/api/materials/${id}/file`,{ headers:authHeaders() })
    if (!res.ok) { let message=`下载失败（${res.status}）`; try{message=(await res.json()).error||message}catch{}; throw new ApiError(res.status,message) }
    const disposition = res.headers.get('Content-Disposition') || ''
    let name = fallbackName
    const utf8 = disposition.match(/filename\*=UTF-8''([^;]+)/i)
    if (utf8) name = decodeURIComponent(utf8[1])
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url; a.download = name
    document.body.appendChild(a); a.click(); a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 4000)
  }
}
