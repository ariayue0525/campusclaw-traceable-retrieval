/** 把回答正文切成「普通文本 / 引用角标」的片段，供页面渲染 [1]、[2] 角标。 */
export type Segment = { kind: 'text'; value: string } | { kind: 'cite'; value: number }

export function splitCitations(text: string): Segment[] {
  const out: Segment[] = []
  const re = /\[(\d+)\]/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push({ kind: 'text', value: text.slice(last, m.index) })
    out.push({ kind: 'cite', value: Number(m[1]) })
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ kind: 'text', value: text.slice(last) })
  return out
}

/** 回答里出现过的角标编号，按出现顺序去重，用于校验出处编号是否完整。 */
export function citedNumbers(text: string): number[] {
  const seen: number[] = []
  for (const s of splitCitations(text)) if (s.kind === 'cite' && !seen.includes(s.value)) seen.push(s.value)
  return seen
}
