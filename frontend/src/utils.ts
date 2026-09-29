export function formatBytes(bytes:number){if(bytes<1024)return `${bytes} B`;if(bytes<1024*1024)return `${(bytes/1024).toFixed(1)} KB`;return `${(bytes/1024/1024).toFixed(1)} MB`}
export function isMarkdown(mediaType:string){return mediaType.toLowerCase().startsWith('text/markdown')}
export function safeTheme(value:string|null):'light'|'dark'{return value==='dark'?'dark':'light'}

