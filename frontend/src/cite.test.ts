import { describe,expect,it } from 'vitest'
import { citedNumbers,splitCitations } from './cite'

describe('回答正文的引用角标',()=>{
  it('把 [1] 解析成角标片段',()=>{
    expect(splitCitations('见资料[1]与[2]。')).toEqual([
      {kind:'text',value:'见资料'},
      {kind:'cite',value:1},
      {kind:'text',value:'与'},
      {kind:'cite',value:2},
      {kind:'text',value:'。'}
    ])
  })
  it('没有角标时原样返回整段文本',()=>expect(splitCitations('资料中未找到相关内容')).toEqual([{kind:'text',value:'资料中未找到相关内容'}]))
  it('按出现顺序去重列出被引用编号',()=>expect(citedNumbers('a[2] b[1] c[2]')).toEqual([2,1]))
  it('不把普通数字当成引用',()=>expect(citedNumbers('共 3 条')).toEqual([]))
})
