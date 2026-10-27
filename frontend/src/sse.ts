export type SSEEvent = { event:string; data:unknown }

export class SSEDecoder {
  private buffer = ''
  push(chunk:string):SSEEvent[]{
    this.buffer += chunk.replace(/\r\n/g,'\n')
    const events:SSEEvent[]=[]
    for(let boundary=this.buffer.indexOf('\n\n');boundary>=0;boundary=this.buffer.indexOf('\n\n')){
      const block=this.buffer.slice(0,boundary)
      this.buffer=this.buffer.slice(boundary+2)
      const lines=block.split('\n')
      const event=lines.find(line=>line.startsWith('event:'))?.slice(6).trim()||'message'
      const data=lines.filter(line=>line.startsWith('data:')).map(line=>line.slice(5).trimStart()).join('\n')
      if(data){events.push({event,data:JSON.parse(data)})}
    }
    return events
  }
}
