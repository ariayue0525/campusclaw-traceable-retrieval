import { describe,expect,it } from 'vitest'
import { formatBytes,isMarkdown,safeTheme } from './utils'
describe('UI utilities',()=>{it('formats sizes',()=>{expect(formatBytes(512)).toBe('512 B');expect(formatBytes(1536)).toBe('1.5 KB')});it('detects markdown',()=>expect(isMarkdown('text/markdown; charset=utf-8')).toBe(true));it('fails closed to light theme',()=>expect(safeTheme('unknown')).toBe('light'))})

