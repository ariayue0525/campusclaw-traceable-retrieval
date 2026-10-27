import { describe,expect,it } from 'vitest'
import { SSEDecoder } from './sse'

describe('SSEDecoder',()=>{
  it('handles split UTF-8-independent text chunks and incremental deltas',()=>{
    const decoder=new SSEDecoder()
    expect(decoder.push('event: meta\ndata: {"citations":[]}\n\nevent: del')).toEqual([{event:'meta',data:{citations:[]}}])
    expect(decoder.push('ta\ndata: {"text":"你好"}\n\nevent: delta\ndata: {"text":"世界"}\n\n')).toEqual([{event:'delta',data:{text:'你好'}},{event:'delta',data:{text:'世界'}}])
  })
  it('parses completion and error events',()=>{
    const decoder=new SSEDecoder()
    expect(decoder.push('event: done\ndata: {"message_id":3}\n\nevent: error\ndata: {"error":"failed"}\n\n')).toEqual([{event:'done',data:{message_id:3}},{event:'error',data:{error:'failed'}}])
  })
})
