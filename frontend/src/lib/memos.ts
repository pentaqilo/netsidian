import {readDocumentTags,writeDocumentTags} from './documentTags';
export function memoMarkdown(text:string){return writeDocumentTags(text.trim(),[...readDocumentTags(text.trim()),'memo']).trimEnd()+'\n'}
export function localDate(date:Date){return `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`}
export function dateBounds(value:string){if(!value)return {};const [y,m,d]=value.split('-').map(Number);return {from:new Date(y,m-1,d).toISOString(),before:new Date(y,m-1,d+1).toISOString()}}
export const legacyMemoPath=/(?:^|\/)\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-\d{3}Z-[a-f0-9]{8}\.md$/;
