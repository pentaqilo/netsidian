import * as api from '../api/client';
import {Icon} from './Icon';

// A PDF (or other previewable attachment) shown as a first-class workspace tab:
// an inline viewer plus a toolbar. "Convert to Markdown" is wired by the caller;
// the original file is never touched — conversion always produces a new note.
export function AttachmentPane({space,path,writable,converting,onConvert,onClose}:{space:string;path:string;writable:boolean;converting:boolean;onConvert:()=>void;onClose:()=>void}){
  const url=api.attachmentURL(space,path);
  const name=path.split('/').pop()||path;
  const isPdf=/\.pdf$/i.test(path);
  return <div className="attachment-pane">
    <div className="attachment-toolbar">
      <span className="attachment-title" title={path}><Icon name="file" size={14}/><span>{name}</span></span>
      <div className="spacer"/>
      {isPdf&&<button className="btn small" disabled={!writable||converting} title={writable?'Extract the PDF text into a new Markdown note; the original PDF is kept':'Read-only vault'} onClick={onConvert}>{converting?'Converting…':'Convert to Markdown'}</button>}
      <a className="btn small" href={url} download>Download</a>
      <button className="btn small" onClick={onClose}>Close</button>
    </div>
    <div className="attachment-body"><iframe title={path} src={url}/></div>
  </div>;
}
