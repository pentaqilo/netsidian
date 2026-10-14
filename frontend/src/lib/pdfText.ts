// PDF.js extracts selectable text in a worker; layout inference and OCR are
// separate operations. The original PDF is never changed by this module.
export interface PdfExtraction {
 text:string;pages:number;hasText:boolean;hasImages:boolean;likelyScanned:boolean;unmapped:boolean;charCount:number;
}
export const MAX_PDF_BYTES=25*1024*1024;
export const MAX_PDF_PAGES=300;
export const MAX_PDF_TEXT=1_000_000;

export async function extractPdfText(bytes:Uint8Array,signal?:AbortSignal):Promise<PdfExtraction> {
 if(bytes.byteLength>MAX_PDF_BYTES)throw new Error('PDF exceeds the 25 MiB conversion limit. The original is unchanged.');
 if(signal?.aborted)throw new DOMException('Conversion cancelled','AbortError');
 const {getDocument,GlobalWorkerOptions}=await import('pdfjs-dist/legacy/build/pdf.mjs');
 if(typeof window!=='undefined'){
  const {default:workerURL}=await import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url');
  GlobalWorkerOptions.workerSrc=workerURL;
 }
 const assets=import.meta.env.DEV?'/node_modules/pdfjs-dist/':'/pdfjs/';
 const task=getDocument({data:bytes.slice(),stopAtErrors:true,disableFontFace:true,useSystemFonts:true,
  cMapUrl:assets+'cmaps/',cMapPacked:true,standardFontDataUrl:assets+'standard_fonts/',wasmUrl:assets+'wasm/'});
 const cancel=()=>{void task.destroy().catch(()=>{})};
 signal?.addEventListener('abort',cancel,{once:true});
 let timedOut=false;
 const timer=setTimeout(()=>{timedOut=true;cancel()},60_000);
 try {
  const pdf=await task.promise;
  if(pdf.numPages>MAX_PDF_PAGES)throw new Error('PDF exceeds the 300-page conversion limit. The original is unchanged.');
  const pages:string[]=[];let count=0;
  for(let n=1;n<=pdf.numPages;n++) {
   if(signal?.aborted)throw new DOMException('Conversion cancelled','AbortError');
   const page=await pdf.getPage(n);const content=await page.getTextContent();
   let line='',text='',lastY:number|undefined;
   for(const item of content.items){
    if(!('str' in item))continue;
    const y=item.transform[5];
    if(lastY!==undefined&&Math.abs(y-lastY)>2&&line.trim()){text+=line.trimEnd()+'\n';line=''}
    if(line&&!/\s$/.test(line)&&item.str&&!/^\s/.test(item.str))line+=' ';
    line+=item.str;lastY=y;
    if(item.hasEOL){text+=line.trimEnd()+'\n';line='';lastY=undefined}
    count+=item.str.length;
    if(count>MAX_PDF_TEXT)throw new Error('Extracted text exceeds the conversion limit. The original is unchanged.');
   }
   text+=line.trimEnd();pages.push(text.trim());page.cleanup();
  }
  const text=pages.join('\n\n').trim();const charCount=text.replace(/\s/g,'').length;
  return {text,pages:pdf.numPages,hasText:charCount>0,charCount,hasImages:false,likelyScanned:charCount===0,unmapped:false};
 }catch(err){
  if(signal?.aborted)throw new DOMException('Conversion cancelled','AbortError');
  if(timedOut)throw new Error('PDF conversion timed out. The original is unchanged.');
  if(err instanceof Error&&err.name==='PasswordException')throw new Error('This PDF is password protected. Unlock it in a PDF reader before converting.');
  throw err;
 }finally{clearTimeout(timer);signal?.removeEventListener('abort',cancel);await task.destroy().catch(()=>{})}
}
const escapeText=(text:string)=>text.replace(/\\/g,'\\\\').replace(/([`*_{}\[\]<>#+!|~-])/g,'\\$1');
export function buildPdfMarkdown(pdfPath:string,ex:PdfExtraction):string {
 if(!ex.hasText||!ex.text.trim())throw new Error('No selectable text was found. This PDF may be scanned or image-only and requires OCR; no Markdown document was created.');
 const base=pdfPath.split('/').pop()||pdfPath;const title=base.replace(/\.pdf$/i,'')||base;
 return '# '+escapeText(title)+'\n\nConverted from [the original PDF](<'+encodeURIComponent(base)+'>). The PDF is preserved.\n\n'+escapeText(ex.text.trim())+'\n';
}

export async function readPdfBytes(response:Response,limit=MAX_PDF_BYTES):Promise<Uint8Array> {
 if(!response.ok)throw new Error('Could not read the PDF.');
 const declared=Number(response.headers.get('Content-Length'));
 if(declared>limit)throw new Error('PDF exceeds the 25 MiB conversion limit.');
 if(!response.body)throw new Error('PDF response has no content.');
 const reader=response.body.getReader();const chunks:Uint8Array[]=[];let size=0;
 try{for(;;){const {value,done}=await reader.read();if(done)break;size+=value.byteLength;
  if(size>limit){await reader.cancel();throw new Error('PDF exceeds the 25 MiB conversion limit.')}
  chunks.push(value);
 }}finally{reader.releaseLock()}
 const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length}return bytes;
}
