import { FormEvent, RefObject, useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { api, ApiError } from './api'
import { splitCitations } from './cite'
import type { AskResult, AskTurn, ChunkOptions, ChunkView, Citation, Hit, Material, SearchMode, SearchResult, User } from './types'
import { formatBytes, isMarkdown, safeTheme } from './utils'

type Toast = { id: number; message: string; kind: 'ok' | 'error' }
type Tab = 'library' | 'search' | 'ask'

const MODE_HINT: Record<SearchMode, { label: string; path: string; desc: string }> = {
  keyword: { label: '关键字', path: 'MySQL 全文索引 → 回表取正文', desc: '只用 MySQL 的 ngram 全文索引匹配，不嵌入、不访问向量库。命中即命中，字面完全一致的片段排在最前。' },
  vector: { label: '向量', path: '查询嵌入 → Qdrant 按班过滤 → 回表取正文', desc: '把问句嵌入成向量，在 Qdrant 里按班级过滤做相似度检索，低于阈值的候选丢弃后再回表。同义改写也能召回。' },
  hybrid: { label: '混合', path: '两条路径各取前 N → RRF 融合（k=60）', desc: '两条路径分别按各自分数过滤，再按名次做倒数排名融合。字面匹配与语义相近同时生效，是默认模式。' }
}

const SEARCH_EXAMPLES = ['考试时间怎么安排', '作业提交的要求是什么', '这门课的成绩怎么算']
const ASK_EXAMPLES = ['这门课什么时候考试？', '作业最晚什么时候交？', '请说明评分标准']

function Brand() {
  return <div className="brand"><span className="brand-mark">CC</span><span><b>CampusClaw</b><small>可追溯知识库检索</small></span></div>
}

function App() {
  const [user, setUser] = useState<User | null>(null)
  const [booting, setBooting] = useState(true)
  const [toasts, setToasts] = useState<Toast[]>([])
  const notify = (message: string, kind: Toast['kind'] = 'ok') => {
    const id = Date.now()
    setToasts(v => [...v, { id, message, kind }])
    setTimeout(() => setToasts(v => v.filter(t => t.id !== id)), 3200)
  }
  useEffect(() => { api.me().then(r => setUser(r.user)).catch(() => setUser(null)).finally(() => setBooting(false)) }, [])
  if (booting) return <div className="boot"><Brand /><span className="spinner" />正在恢复身份…</div>
  return <>
    <div className="ambient ambient-a" /><div className="ambient ambient-b" />
    {user ? <Workspace user={user} onLogout={() => setUser(null)} notify={notify} /> : <Login onLogin={setUser} notify={notify} />}
    <div className="toasts" aria-live="polite">{toasts.map(t => <div key={t.id} className={`toast ${t.kind}`}>{t.kind === 'ok' ? '✓' : '!'} {t.message}</div>)}</div>
  </>
}

function Login({ onLogin, notify }: { onLogin: (u: User) => void; notify: (m: string, k?: Toast['kind']) => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(e: FormEvent) {
    e.preventDefault(); setBusy(true)
    try { const r = await api.login(username, password); onLogin(r.user); notify(`欢迎回来，${r.user.displayName}`) }
    catch (error) { notify(error instanceof Error ? error.message : '登录失败', 'error') }
    finally { setBusy(false) }
  }
  return <main className="login-shell">
    <section className="login-story"><Brand /><p className="eyebrow">TRACEABLE RETRIEVAL · CLASS SCOPED</p><h1>每一句回答<br /><em>都能回到原文</em></h1><p className="lead">切片、嵌入、向量库与关系库协同工作：检索范围被限定在本班，答案附带的每个编号都能点回原始段落。</p><div className="trust-row"><span>● 班级过滤前置</span><span>● 混合检索 RRF</span><span>● 出处可回溯</span></div></section>
    <section className="login-panel"><form onSubmit={submit} className="login-card"><p className="eyebrow">WELCOME BACK</p><h2>登录 CampusClaw</h2><p className="muted">使用课程预置账号进入你的班级空间</p><label>用户名<input autoFocus autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} placeholder="例如 teacher-a" required /></label><label>密码<input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} placeholder="输入环境变量中设置的口令" required /></label><button className="primary" disabled={busy}>{busy ? <><span className="spinner" />正在验证…</> : '安全登录 →'}</button><div className="login-note"><span>🔒</span><p><b>检索范围由服务端决定</b><br />浏览器传来的班级编号会被丢弃，只认会话里的班级</p></div></form></section>
  </main>
}

function Workspace({ user, onLogout, notify }: { user: User; onLogout: () => void; notify: (m: string, k?: Toast['kind']) => void }) {
  const [tab, setTab] = useState<Tab>('library')
  const [items, setItems] = useState<Material[]>([])
  const [query, setQuery] = useState('')
  const [view, setView] = useState<'grid' | 'list'>('grid')
  const [selected, setSelected] = useState<Material | null>(null)
  const [loading, setLoading] = useState(true)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [theme, setTheme] = useState<'light' | 'dark'>(() => safeTheme(localStorage.getItem('theme')))
  const searchRef = useRef<HTMLInputElement>(null)
  const stats = useMemo(() => ({ count: items.length, total: items.reduce((n, m) => n + m.sizeBytes, 0) }), [items])

  const load = async (q = query) => {
    setLoading(true)
    try { setItems((await api.materials(q)).materials) }
    catch (error) { if (error instanceof ApiError && error.status === 401) onLogout(); else notify(error instanceof Error ? error.message : '加载失败', 'error') }
    finally { setLoading(false) }
  }
  useEffect(() => { void load('') }, [])
  useEffect(() => { document.documentElement.dataset.theme = theme; localStorage.setItem('theme', theme) }, [theme])
  async function openMaterial(m: Material) { try { setSelected((await api.material(m.id)).material) } catch (error) { notify(error instanceof Error ? error.message : '读取失败', 'error') } }
  async function logout() { try { await api.logout() } finally { onLogout() } }

  return <div className="app-shell">
    <aside className="sidebar">
      <Brand />
      <nav>
        <button className={tab === 'library' ? 'nav-active' : ''} onClick={() => setTab('library')}>▦<span>材料库</span><i>{items.length}</i></button>
        <button className={tab === 'search' ? 'nav-active' : ''} onClick={() => setTab('search')}>⌕<span>知识检索</span></button>
        <button className={tab === 'ask' ? 'nav-active' : ''} onClick={() => setTab('ask')}>✦<span>班级问答</span></button>
      </nav>
      <div className="side-foot">
        <div className="class-card"><span>检索范围</span><b>{user.className}</b><small>{user.role === 'teacher' ? '教师管理权限' : '学生只读权限'}</small></div>
        <button className="user-card" onClick={logout}><span className="avatar">{user.displayName.slice(0, 1)}</span><span><b>{user.displayName}</b><small>{user.username} · 退出</small></span></button>
      </div>
    </aside>
    <main className="workspace">
      <header className="topbar">
        <div><p className="eyebrow">{user.className} · TRACEABLE KNOWLEDGE BASE</p><h1>{tab === 'library' ? '教研材料' : tab === 'search' ? '知识检索' : '班级问答'}</h1></div>
        <div className="top-actions"><button className="icon-btn" title="切换主题" onClick={() => setTheme(v => v === 'light' ? 'dark' : 'light')}>{theme === 'light' ? '☾' : '☀'}</button>{user.role === 'teacher' && <button className="primary compact" onClick={() => setUploadOpen(true)}>＋ 上传材料</button>}</div>
      </header>
      {tab === 'library' && <LibraryTab
        items={items} loading={loading} query={query} setQuery={setQuery} view={view} setView={setView}
        searchRef={searchRef} stats={stats} user={user} onSearch={() => void load(query)} onOpen={openMaterial}
        onReload={() => void load('')} onUpload={() => setUploadOpen(true)}
      />}
      {tab === 'search' && <SearchTab user={user} notify={notify} onOpen={openMaterial} />}
      {tab === 'ask' && <AskTab user={user} />}
      <footer><span>CampusClaw · 第 4 课 · 可追溯知识库检索</span><span>正文来自 MySQL · 向量来自 Qdrant · 班级边界由服务端判定</span></footer>
    </main>
    {selected && <Detail material={selected} user={user} onClose={() => setSelected(null)} notify={notify} />}
    {uploadOpen && <Upload onClose={() => setUploadOpen(false)} onDone={() => void load()} notify={notify} />}
  </div>
}

// ---------- 材料库 ----------

function LibraryTab({ items, loading, query, setQuery, view, setView, searchRef, stats, user, onSearch, onOpen, onReload, onUpload }: {
  items: Material[]; loading: boolean; query: string; setQuery: (v: string) => void; view: 'grid' | 'list'; setView: (v: 'grid' | 'list') => void
  searchRef: RefObject<HTMLInputElement>; stats: { count: number; total: number }; user: User
  onSearch: () => void; onOpen: (m: Material) => void; onReload: () => void; onUpload: () => void
}) {
  return <>
    <section className="hero-strip">
      <div><span>本班材料</span><strong>{stats.count}</strong><small>份已入库文档</small></div>
      <div><span>存储用量</span><strong>{formatBytes(stats.total)}</strong><small>原文件安全保存</small></div>
      <div className="hero-message"><p>“检索的范围，就是班级的边界。”</p><small>所有查询都以服务端班级身份为边界</small></div>
    </section>
    <section className="toolbar">
      <form onSubmit={e => { e.preventDefault(); onSearch() }} className="search"><span>⌕</span><input ref={searchRef} value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索本班材料标题或文件名…" /><button>搜索</button></form>
      <div className="segmented"><button className={view === 'grid' ? 'active' : ''} onClick={() => setView('grid')}>▦</button><button className={view === 'list' ? 'active' : ''} onClick={() => setView('list')}>☷</button></div>
    </section>
    <section className="material-section">
      <div className="section-head"><h2>最近材料</h2><span>{query ? `“${query}”的搜索结果` : '按入库时间排序'}</span></div>
      {loading ? <div className="empty"><span className="spinner" />正在查询数据库…</div> : items.length === 0 ? <div className="empty"><b>没有找到材料</b><span>换个关键词，或由教师上传第一份文档。</span>{user.role === 'teacher' && <button className="primary compact" onClick={onUpload}>上传材料</button>}<button onClick={onReload}>刷新列表</button></div> : <div className={`materials ${view}`}>{items.map((m, i) => <button className="material-card" style={{ animationDelay: `${i * 45}ms` }} key={m.id} onClick={() => onOpen(m)}><span className={`file-icon ${isMarkdown(m.mediaType) ? 'md' : 'txt'}`}>{isMarkdown(m.mediaType) ? 'MD' : 'TXT'}</span><span className="material-main"><b>{m.title}</b><small>{m.originalName}</small><span className="meta"><i>{m.uploaderName}</i><i>{new Date(m.createdAt).toLocaleDateString('zh-CN')}</i><i>{formatBytes(m.sizeBytes)}</i></span></span><span className="arrow">↗</span></button>)}</div>}
    </section>
  </>
}

// ---------- 检索 ----------

function SearchTab({ user, notify, onOpen }: { user: User; notify: (m: string, k?: Toast['kind']) => void; onOpen: (m: Material) => void }) {
  const [q, setQ] = useState('')
  const [mode, setMode] = useState<SearchMode>('hybrid')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<SearchResult | null>(null)

  async function run(value: string) {
    const text = value.trim()
    if (!text) { notify('请输入检索内容', 'error'); return }
    setBusy(true)
    try { setResult(await api.search(text, mode)) }
    catch (error) { notify(error instanceof Error ? error.message : '检索失败', 'error'); setResult(null) }
    finally { setBusy(false) }
  }

  return <>
    <section className="retrieval-hero">
      <div className="rh-copy"><p className="eyebrow">RETRIEVAL SCOPE</p><h2>限定在“{user.className}”内检索</h2><p className="muted">检索范围只来自你的登录会话。请求里即便携带别的班级编号，服务端也不会采用。</p></div>
      <div className="rh-modes">{Object.entries(MODE_HINT).map(([key, m]) => <button key={key} className={`mode-chip ${mode === key ? 'active' : ''}`} onClick={() => setMode(key as SearchMode)}><b>{m.label}</b><small>{m.path}</small></button>)}</div>
    </section>
    <section className="ask-form">
      <form onSubmit={e => { e.preventDefault(); void run(q) }} className="search"><span>⌕</span><input value={q} onChange={e => setQ(e.target.value)} placeholder="输入一句自然语言问题，例如：考试时间怎么安排" /><button disabled={busy}>{busy ? '检索中…' : '检索'}</button></form>
      <div className="examples">{SEARCH_EXAMPLES.map(e => <button key={e} onClick={() => { setQ(e); void run(e) }}>{e}</button>)}</div>
    </section>
    <section className="trace-panel">
      <p className="eyebrow">本条查询走过的路径</p>
      <div className="trace-flow">{MODE_HINT[mode].path.split('→').map((s, i, arr) => <span key={i}>{s.trim()}{i < arr.length - 1 && <i>→</i>}</span>)}</div>
      <p className="muted">{MODE_HINT[mode].desc}</p>
    </section>
    {result && <SearchResultView result={result} onOpen={onOpen} />}
  </>
}

function SearchResultView({ result, onOpen }: { result: SearchResult; onOpen: (m: Material) => void }) {
  if (result.status === 'no_evidence' || result.hits.length === 0) {
    return <section className="empty"><b>{result.message || '资料中未找到相关内容'}</b><span>换个问法，或换一种检索模式再试。</span></section>
  }
  return <section className="material-section">
    <div className="section-head"><h2>命中切片</h2><span>共 {result.total} 条 · 模式 {result.mode} · 嵌入 {result.embedder} · 集合 {result.collection}</span></div>
    <div className="hits">
      {result.hits.map(h => <HitCard key={h.chunk_id} hit={h} onOpen={onOpen} />)}
    </div>
    {result.query_vector.length > 0 && <div className="vector-trace"><p className="eyebrow">问句向量（前 8 维）</p><code>[{result.query_vector.slice(0, 8).map(n => n.toFixed(4)).join(', ')} …]</code><small>维度 {result.query_vector.length} · 由嵌入器 {result.embedder} 生成，仅用于相似度计算，不回显给其他班级</small></div>}
  </section>
}

function HitCard({ hit, onOpen }: { hit: Hit; onOpen: (m: Material) => void }) {
  const parts: string[] = []
  if (hit.keyword_rank != null) parts.push(`关键字第 ${hit.keyword_rank} 名`)
  if (hit.vector_rank != null) parts.push(`向量第 ${hit.vector_rank} 名`)
  return <article className="hit-card">
    <header>
      <span className="rank">#{hit.rank}</span>
      <button className="hit-title" onClick={() => onOpen({ id: hit.material_id } as Material)}>{hit.material_title}</button>
      <span className="hit-path">第 {hit.chunk_index + 1} 段 · 字符 {hit.start_offset}–{hit.end_offset}</span>
    </header>
    <p className="hit-snippet">{hit.snippet}</p>
    <footer>
      <span>融合分 {hit.score.toFixed(4)}</span>
      {hit.keyword_score != null && <span>关键字 {hit.keyword_score.toFixed(3)}</span>}
      {hit.vector_score != null && <span>向量 {hit.vector_score.toFixed(3)}</span>}
      {parts.length > 0 && <span className="hit-ranks">{parts.join(' · ')}</span>}
      <span className="hit-ids">chunk #{hit.chunk_id}</span>
    </footer>
  </article>
}

// ---------- 问答 ----------

function AskTab({ user }: { user: User }) {
  const [q, setQ] = useState('')
  const [busy, setBusy] = useState(false)
  const [turns, setTurns] = useState<{ role: 'user' | 'assistant'; question?: string; result?: AskResult }[]>([])

  async function run(value: string) {
    const text = value.trim()
    if (!text) return
    setBusy(true)
    const history: AskTurn[] = turns.flatMap(t => t.question ? [{ role: 'user' as const, content: t.question }, { role: 'assistant' as const, content: t.result?.answer ?? '' }] : [])
    setTurns(v => [...v, { role: 'user', question: text }])
    setQ('')
    try {
      const result = await api.ask(text, history)
      setTurns(v => [...v, { role: 'assistant', result }])
    } catch (error) {
      setTurns(v => [...v, { role: 'assistant', result: { answer: error instanceof Error ? error.message : '问答失败', status: 'error', citations: [], mode: 'hybrid' } }])
    } finally { setBusy(false) }
  }

  return <>
    <section className="retrieval-hero">
      <div className="rh-copy"><p className="eyebrow">ANSWER WITH EVIDENCE</p><h2>只依据“{user.className}”的资料作答</h2><p className="muted">先在本班做混合检索取得切片；没有命中时直接返回固定文案，不会调用生成模型。</p></div>
      <div className="rh-note"><b>回答里的 [1]、[2]</b><span>与下方出处列表一一对应，点开即可看到原文段落与字符位置。</span></div>
    </section>
    <div className="chat-log">
      {turns.length === 0 && <div className="empty"><b>向本班资料提一个问题</b><div className="examples">{ASK_EXAMPLES.map(e => <button key={e} onClick={() => void run(e)}>{e}</button>)}</div></div>}
      {turns.map((t, i) => t.role === 'user'
        ? <div className="chat-turn user" key={i}><div className="bubble">{t.question}</div></div>
        : <div className="chat-turn assistant" key={i}><div className="bubble"><AnswerText text={t.result?.answer ?? ''} /></div>{t.result && <CitationList citations={t.result.citations} status={t.result.status} />}</div>)}
      {busy && <div className="chat-turn assistant"><div className="bubble"><span className="spinner" /> 正在检索本班资料并生成回答…</div></div>}
    </div>
    <form className="ask-form" onSubmit={e => { e.preventDefault(); void run(q) }}>
      <div className="search"><span>✦</span><input value={q} onChange={e => setQ(e.target.value)} placeholder="输入你的问题…" /><button disabled={busy}>{busy ? '思考中…' : '提问'}</button></div>
    </form>
  </>
}

/** 把回答里的 [1]、[2] 渲染成角标，编号与出处列表一一对应。 */
function AnswerText({ text }: { text: string }) {
  return <p className="answer-text">{splitCitations(text).map((s, i) => s.kind === 'text' ? s.value : <sup className="cite" key={i}>{s.value}</sup>)}</p>
}

function CitationList({ citations, status }: { citations: Citation[]; status: string }) {
  if (status === 'no_evidence') return <div className="citations empty-cite">本次没有检索到可用依据，因此没有调用生成模型，也不会给出猜测。</div>
  if (citations.length === 0) return null
  return <div className="citations"><p className="eyebrow">出处（{citations.length}）</p>{citations.map((c, i) => <div className="citation" key={c.chunk_id}><span className="cite-badge">{i + 1}</span><div><b>{c.material_title}</b><small>第 {c.chunk_index + 1} 段 · 字符 {c.start_offset}–{c.end_offset} · chunk #{c.chunk_id}</small><p>{c.snippet}</p></div></div>)}</div>
}

// ---------- 材料详情 ----------

function Detail({ material, user, onClose, notify }: { material: Material; user: User; onClose: () => void; notify: (m: string, k?: Toast['kind']) => void }) {
  const [panel, setPanel] = useState<'doc' | 'chunks'>('doc')
  const [chunks, setChunks] = useState<ChunkView[]>([])
  const [collection, setCollection] = useState('')
  const [loading, setLoading] = useState(false)
  const [strategy, setStrategy] = useState('auto')
  const [separator, setSeparator] = useState('newline')
  const [maxChars, setMaxChars] = useState(900)
  const [overlap, setOverlap] = useState(15)
  const [collapseWs, setCollapseWs] = useState(true)
  const [stripLinks, setStripLinks] = useState(false)
  const [busy, setBusy] = useState(false)

  const loadChunks = async () => {
    setLoading(true)
    try { const r = await api.chunkVectors(material.id); setChunks(r.chunks); setCollection(r.collection) }
    catch (error) { notify(error instanceof Error ? error.message : '读取切片失败', 'error') }
    finally { setLoading(false) }
  }
  useEffect(() => { if (panel === 'chunks') void loadChunks() }, [panel, material.id])

  async function reindex() {
    setBusy(true)
    try {
      const r = await api.reindex(material.id, {
        chunk_strategy: strategy, chunk_separator: separator, chunk_max_chars: maxChars,
        chunk_overlap_percent: overlap, chunk_collapse_ws: collapseWs ? 1 : 0, chunk_strip_links: stripLinks ? 1 : 0
      })
      notify(`已重建索引：${r.chunk_count} 个切片，其中 ${r.ready_count} 个已写入向量库`)
      void loadChunks()
    } catch (error) { notify(error instanceof Error ? error.message : '重建索引失败', 'error') }
    finally { setBusy(false) }
  }

  const ready = chunks.filter(c => c.index_status === 'ready').length
  const linked = chunks.filter(c => c.linked).length

  return <div className="overlay" onMouseDown={e => { if (e.currentTarget === e.target) onClose() }}><article className="drawer">
    <header><div><span className={`file-icon ${isMarkdown(material.mediaType) ? 'md' : 'txt'}`}>{isMarkdown(material.mediaType) ? 'MD' : 'TXT'}</span><span><p className="eyebrow">MATERIAL DETAIL</p><h2>{material.title}</h2></span></div><button className="icon-btn" onClick={onClose}>×</button></header>
    <div className="detail-meta"><span>上传者<b>{material.uploaderName}</b></span><span>文件<b>{material.originalName}</b></span><span>大小<b>{formatBytes(material.sizeBytes)}</b></span></div>
    <div className="segmented tabs"><button className={panel === 'doc' ? 'active' : ''} onClick={() => setPanel('doc')}>原文</button><button className={panel === 'chunks' ? 'active' : ''} onClick={() => setPanel('chunks')}>切片与向量</button></div>
    {panel === 'doc'
      ? <div className="document">{isMarkdown(material.mediaType) ? <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml>{material.content || ''}</ReactMarkdown> : <pre>{material.content}</pre>}</div>
      : <div className="document">
        <div className="chunk-summary">
          <span>切片总数<b>{chunks.length}</b></span><span>已入向量库<b>{ready}</b></span><span>两库主键对齐<b>{linked}</b></span><span>集合<b>{collection || '—'}</b></span>
        </div>
        {loading ? <div className="empty"><span className="spinner" />正在读取切片…</div> : chunks.length === 0 ? <div className="empty"><b>这份材料还没有切片</b><span>教师可以用下方的策略重建索引。</span></div> : chunks.map(c => <ChunkCompare key={c.mysql.chunk_id} chunk={c} />)}
      </div>}
    {user.role === 'teacher' && panel === 'chunks' && <div className="reindex-box">
      <p className="eyebrow">按策略重建索引</p>
      <div className="reindex-form">
        <label>策略<select value={strategy} onChange={e => setStrategy(e.target.value)}><option value="auto">auto · 自动</option><option value="custom">custom · 自定义分隔符</option><option value="hierarchy">hierarchy · 按标题层级</option></select></label>
        <label>分隔符<select value={separator} onChange={e => setSeparator(e.target.value)} disabled={strategy !== 'custom'}><option value="newline">换行</option><option value="blankline">空行</option><option value="paragraph">段落</option><option value="sentence">句号</option></select></label>
        <label>最大字符<input type="number" min={200} max={4000} value={maxChars} onChange={e => setMaxChars(Number(e.target.value))} /></label>
        <label>重叠 %<input type="number" min={0} max={50} value={overlap} onChange={e => setOverlap(Number(e.target.value))} /></label>
        <label className="check"><input type="checkbox" checked={collapseWs} onChange={e => setCollapseWs(e.target.checked)} />合并空白</label>
        <label className="check"><input type="checkbox" checked={stripLinks} onChange={e => setStripLinks(e.target.checked)} />去除链接</label>
      </div>
      <button className="primary compact" disabled={busy} onClick={() => void reindex()}>{busy ? '重建中…' : '重新切分并写入向量库'}</button>
    </div>}
    <div className="drawer-actions"><a className="primary" href={api.fileUrl(material.id)}>↓ 下载原文件</a><button onClick={onClose}>关闭</button></div>
  </article></div>
}

/** 同一条切片在 MySQL 与 Qdrant 里各存了什么，并排展示。 */
function ChunkCompare({ chunk }: { chunk: ChunkView }) {
  return <div className="chunk-compare">
    <div className="cc-head"><span className="cc-index">#{chunk.chunk_index + 1}</span><span className={`status ${chunk.index_status}`}>{chunk.index_status}</span>{chunk.linked ? <span className="linked">两库对齐</span> : <span className="unlinked">缺少向量</span>}</div>
    <p className="cc-text">{chunk.excerpt}</p>
    <div className="cc-grid">
      <div className="cc-col"><p className="eyebrow">MySQL · knowledge_chunks</p><dl><dt>chunk_id</dt><dd>{chunk.mysql.chunk_id}</dd><dt>entry_id</dt><dd>{chunk.mysql.knowledge_entry_id}</dd><dt>material_id</dt><dd>{chunk.mysql.material_id}</dd><dt>class_id</dt><dd>{chunk.mysql.class_id}</dd><dt>字符区间</dt><dd>{chunk.start_offset}–{chunk.end_offset}</dd></dl><p className="cc-body">{chunk.chunk_text}</p></div>
      <div className="cc-col"><p className="eyebrow">Qdrant · {chunk.collection}</p>{chunk.vector_point ? <><dl><dt>point_id</dt><dd>{chunk.vector_point.point_id}</dd><dt>payload.chunk_id</dt><dd>{chunk.vector_point.payload_chunk_id}</dd><dt>payload.class_id</dt><dd>{chunk.vector_point.payload_class_id}</dd><dt>dim</dt><dd>{chunk.vector_point.dim}</dd><dt>norm</dt><dd>{chunk.vector_point.norm.toFixed(4)}</dd></dl><code className="cc-vector">[{chunk.vector_point.preview.map(n => n.toFixed(4)).join(', ')} …]</code><small>这里只存浮点向量与标识，正文始终回表取</small></> : <p className="muted">向量库中没有这条切片的点，可能是嵌入失败或尚未重建索引。</p>}</div>
    </div>
  </div>
}

function Upload({ onClose, onDone, notify }: { onClose: () => void; onDone: () => void; notify: (m: string, k?: Toast['kind']) => void }) {
  const [file, setFile] = useState<File | null>(null), [title, setTitle] = useState(''), [progress, setProgress] = useState(0), [busy, setBusy] = useState(false)
  const [strategy, setStrategy] = useState('auto'), [separator, setSeparator] = useState('newline'), [maxChars, setMaxChars] = useState(900), [overlap, setOverlap] = useState(15)
  async function submit(e: FormEvent) { e.preventDefault(); if (!file) return; setBusy(true); try { await api.upload(file, title, { chunk_strategy: strategy, chunk_separator: separator, chunk_max_chars: maxChars, chunk_overlap_percent: overlap }, setProgress); notify('材料已切分、嵌入并写入向量库'); onDone(); onClose() } catch (error) { notify(error instanceof Error ? error.message : '上传失败', 'error'); setBusy(false) } }
  return <div className="overlay" onMouseDown={e => { if (e.currentTarget === e.target && !busy) onClose() }}><form className="modal" onSubmit={submit}>
    <header><div><p className="eyebrow">TEACHER ACTION</p><h2>上传教研材料</h2></div><button type="button" className="icon-btn" onClick={onClose}>×</button></header>
    <label className="drop"><input type="file" accept=".txt,.md,text/plain,text/markdown" onChange={e => setFile(e.target.files?.[0] || null)} required /><span>{file ? '✓' : '＋'}</span><b>{file ? file.name : '选择 .txt 或 .md 文件'}</b><small>{file ? formatBytes(file.size) : 'UTF-8 编码，最大 2 MB'}</small></label>
    <label>材料标题（可选）<input value={title} onChange={e => setTitle(e.target.value)} placeholder="留空时使用文件名" maxLength={120} /></label>
    <div className="reindex-form compact-form">
      <label>切分策略<select value={strategy} onChange={e => setStrategy(e.target.value)}><option value="auto">auto · 自动</option><option value="custom">custom · 自定义分隔符</option><option value="hierarchy">hierarchy · 按标题层级</option></select></label>
      <label>分隔符<select value={separator} onChange={e => setSeparator(e.target.value)} disabled={strategy !== 'custom'}><option value="newline">换行</option><option value="blankline">空行</option><option value="paragraph">段落</option><option value="sentence">句号</option></select></label>
      <label>最大字符<input type="number" min={200} max={4000} value={maxChars} onChange={e => setMaxChars(Number(e.target.value))} /></label>
      <label>重叠 %<input type="number" min={0} max={50} value={overlap} onChange={e => setOverlap(Number(e.target.value))} /></label>
    </div>
    {busy && <div className="progress"><span style={{ width: `${progress}%` }} /><b>{progress}% · 正在切分并写入向量库</b></div>}
    <div className="modal-actions"><button type="button" onClick={onClose} disabled={busy}>取消</button><button className="primary" disabled={!file || busy}>{busy ? '上传中…' : '上传并建索引'}</button></div>
  </form></div>
}

export default App
