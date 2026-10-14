import {describe,it,expect} from 'vitest';
import {deflateSync} from 'node:zlib';
import {extractPdfText,buildPdfMarkdown,MAX_PDF_BYTES,readPdfBytes,type PdfExtraction} from './pdfText';

const enc=(s:string)=>new TextEncoder().encode(s);
function concat(parts:Uint8Array[]){const out=new Uint8Array(parts.reduce((n,p)=>n+p.length,0));let offset=0;for(const p of parts){out.set(p,offset);offset+=p.length}return out}
function stream(data:Uint8Array,compressed=false){return concat([enc(`<< /Length ${data.length}${compressed?' /Filter /FlateDecode':''} >>\nstream\n`),data,enc('\nendstream')])}
function fixture(text:string|null,compressed=false):Uint8Array {
 const literal=(text||'').replace(/([\\()])/g,'\\$1');
 const data=enc(text===null?'':`BT /F1 18 Tf 72 720 Td (${literal}) Tj ET`);
 const objects=[enc('<< /Type /Catalog /Pages 2 0 R >>'),enc('<< /Type /Pages /Kids [3 0 R] /Count 1 >>'),enc('<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>'),enc('<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>'),stream(compressed?deflateSync(data):data,compressed)];
 const parts=[enc('%PDF-1.4\n')],offsets=[0];let size=parts[0].length;
 objects.forEach((obj,i)=>{offsets.push(size);const p=concat([enc(`${i+1} 0 obj\n`),obj,enc('\nendobj\n')]);parts.push(p);size+=p.length});
 const xref=size;parts.push(enc('xref\n0 6\n0000000000 65535 f \n'+offsets.slice(1).map(n=>String(n).padStart(10,'0')+' 00000 n \n').join('')+`trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`));return concat(parts);
}
describe('PDF conversion',()=>{
 for(const compressed of [false,true])it(`extracts a valid ${compressed?'compressed':'plain'} PDF text layer`,async()=>{
  const bytes=fixture('PDF conversion acceptance text',compressed),original=bytes.slice();
  const ex=await extractPdfText(bytes);expect(ex.text).toBe('PDF conversion acceptance text');expect(ex.pages).toBe(1);expect(ex.hasText).toBe(true);expect(bytes).toEqual(original);
 });
 it('accepts short text rather than classifying a short document as scanned',async()=>{expect((await extractPdfText(fixture('Hello'))).text).toBe('Hello')});
 it('does not create misleading Markdown for a PDF with no text layer',async()=>{const ex=await extractPdfText(fixture(null));expect(ex.hasText).toBe(false);expect(()=>buildPdfMarkdown('blank.pdf',ex)).toThrow(/requires OCR/)});
 it('rejects malformed, oversized and cancelled input',async()=>{
  await expect(extractPdfText(enc('not a PDF'))).rejects.toThrow();
  await expect(extractPdfText(new Uint8Array(MAX_PDF_BYTES+1))).rejects.toThrow(/limit/);
  const c=new AbortController();c.abort();await expect(extractPdfText(fixture('Hello'),c.signal)).rejects.toMatchObject({name:'AbortError'});
 });
 it('preserves literal PDF text and source filename in Markdown',()=>{
  const ex:PdfExtraction={text:'<script>alert(1)</script>\n# text [link](https://example.test)',pages:1,hasText:true,hasImages:false,likelyScanned:false,unmapped:false,charCount:40};
  const md=buildPdfMarkdown('Reports/Quarterly Report.pdf',ex);
  expect(md).toContain('# Quarterly Report');expect(md).toContain('Quarterly%20Report.pdf');expect(md).toContain('PDF is preserved');expect(md).toContain('\\<script\\>');expect(md).toContain('\\# text \\[link\\]');
 });
});

it('bounds streamed PDF downloads before allocating the full response',async()=>{
 const response=new Response(new ReadableStream({start(c){c.enqueue(new Uint8Array(8));c.enqueue(new Uint8Array(9));c.close()}}));
 await expect(readPdfBytes(response,16)).rejects.toThrow(/limit/);
 await expect(readPdfBytes(new Response('x',{headers:{'Content-Length':'20'}}),16)).rejects.toThrow(/limit/);
});
it('reads a bounded chunked PDF download without changing bytes',async()=>{expect(await readPdfBytes(new Response(new Uint8Array([1,2,3])),16)).toEqual(new Uint8Array([1,2,3]))});
