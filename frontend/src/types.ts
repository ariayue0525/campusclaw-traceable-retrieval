export type User = { id:number; username:string; displayName:string; role:'teacher'|'student'; classId:number; className:string }
export type Material = { id:number; classId:number; uploaderId:number; uploaderName:string; title:string; originalName:string; mediaType:string; sizeBytes:number; content?:string; createdAt:string }

export type SearchMode = 'keyword' | 'vector' | 'hybrid'

/** 一条可回溯的命中结果：正文只来自 MySQL，向量库只提供分数与主键。 */
export type Hit = {
  chunk_id:number
  material_id:number
  material_title:string
  knowledge_entry_id:number
  chunk_index:number
  start_offset:number
  end_offset:number
  snippet:string
  score:number
  keyword_score?:number|null
  vector_score?:number|null
  keyword_rank?:number|null
  vector_rank?:number|null
  rank:number
}

export type SearchResult = {
  mode:string
  q:string
  status:string
  message?:string
  hits:Hit[]
  total:number
  query_vector:number[]
  collection:string
  embedder:string
}

export type Citation = {
  chunk_id:number
  material_id:number
  material_title:string
  chunk_index:number
  start_offset:number
  end_offset:number
  snippet:string
}

export type AskTurn = { role:'user'|'assistant'; content:string }

/** answer_source 区分回答来自对话网关（chat_gateway）还是服务端原文摘录（extract）。 */
export type AskResult = { answer:string; status:string; citations:Citation[]; mode:string; answer_source?:string }

/** MySQL 一侧的切片标识。 */
export type MySQLRef = { chunk_id:number; knowledge_entry_id:number; material_id:number; class_id:number }

/** 向量库一侧的切片标识与形态：只有浮点向量，没有正文。 */
export type PointView = {
  point_id:number
  payload_chunk_id:number
  payload_entry_id:number
  payload_material_id:number
  payload_class_id:number
  payload_chunk_index:number
  dim:number
  norm:number
  preview:number[]
}

export type ChunkView = {
  chunk_index:number
  index_status:string
  start_offset:number
  end_offset:number
  excerpt:string
  chunk_text:string
  mysql:MySQLRef
  vector_point:PointView|null
  linked:boolean
  collection:string
}

export type ChunkVectorsResponse = {
  material_id:number
  class_id:number
  collection:string
  chunks:ChunkView[]
}

export type ReindexResult = {
  id:number
  title:string
  chunk_count:number
  chunk_strategy:string
  ready_count:number
  failed_count:number
}

export type ChunkOptions = {
  chunk_strategy?:string
  chunk_separator?:string
  chunk_max_chars?:number
  chunk_overlap_percent?:number
  chunk_collapse_ws?:number
  chunk_strip_links?:number
}

export type Agent = { id:number; class_id:number; name:string; prompt:string; prompt_template_id?:number|null; enabled:boolean; skill_ids:number[]; material_ids:number[]; created_at:string; updated_at:string }
export type Skill = { id:number; class_id:number; name:string; trigger_text:string; subject_text:string; description:string; tools:string[]; source_text:string; enabled:boolean; created_at:string; updated_at:string }
export type PromptTemplate = { id:number; class_id:number; name:string; content:string; enabled:boolean }
export type Conversation = { id:number; class_id:number; user_id:number; agent_id:number; title:string; created_at:string; updated_at:string }
export type ChatMessage = { id:number; conversation_id:number; role:'user'|'assistant'; content:string; citations:Citation[]; skill_name?:string; created_at:string }
export type MCPServer = { id:number; class_id:number; name:string; base_url:string; has_token:boolean; connected:boolean }
export type MCPTool = { id:number; server_id:number; name:string; description:string; input_schema:unknown }
export type MCPCall = { id:number; server_id:number; tool_name:string; status:string; detail:string; created_at:string }
