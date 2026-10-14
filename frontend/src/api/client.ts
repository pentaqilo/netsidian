// Typed API client for the /api/v1 contract. Mutations always send the
// X-Netsidian-Requested header (CSRF guard); cookie session auth is the
// default. One ApiError type carries the stable error codes.

import type {
  InstanceSettings,
  VaultStorageConfig,
  AttachedVault,
  Backlink,
  DocMeta,
  GraphData,
  AnalysisGraph,
  NoteContent,
  RevisionMeta,
  SearchHit,
  SpaceWithRole,
  TagCount,
  WorkspaceState,
} from "./types";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public retryable: boolean,
    public currentRevision?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }

  static isConflict(e: unknown): e is ApiError {
    return e instanceof ApiError && (e.status === 412 || e.status === 428);
  }
}

const BASE = "/api/v1";

async function request<T>(
  method: string,
  path: string,
  opts: { signal?:AbortSignal; emptyQueryKeys?:readonly string[]; body?: unknown; idempotencyKey?:string; ifMatch?: string; query?: Record<string, string | number | undefined> } = {},
): Promise<T> {
  let url = BASE + path;
  if (opts.query) {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(opts.query)) {
      if (v !== undefined && (v !== "" || opts.emptyQueryKeys?.includes(k))) qs.set(k, String(v));
    }
    const q = qs.toString();
    if (q) url += "?" + q;
  }
  const headers: Record<string, string> = { "X-Netsidian-Requested": "1" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if(opts.idempotencyKey)headers["Idempotency-Key"]=opts.idempotencyKey;
  if (opts.ifMatch) headers["If-Match"] = opts.ifMatch;
  const res = await fetch(url, {
    signal: opts.signal,
    method,
    credentials: "same-origin",
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
  });
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const e = data?.error ?? {};
    throw new ApiError(res.status, e.code ?? "unknown", e.message ?? res.statusText, !!e.retryable, e.currentRevision);
  }
  return data as T;
}

// The Go server encodes empty result sets as JSON null (nil slices), so every
// list-bearing response is normalized here at the API boundary — consumers
// always see real arrays and never a null-coalescing footgun.
function normList<T>(v: T[] | null | undefined): T[] {
  return v ?? [];
}

// ---- auth / account ----
export const getSettings = () =>
  request<InstanceSettings>("GET", "/settings");
export const setup = (email: string, password: string, displayName: string) =>
  request<{ userId: string; displayName: string }>("POST", "/auth/setup", { body: { email, password, displayName } });
export const signup = (email: string, password: string, displayName: string) =>
  request<{ userId: string; displayName: string }>("POST", "/auth/signup", { body: { email, password, displayName } });
export const login = (email: string, password: string) =>
  request<{ userId: string; displayName: string; challenge?: boolean; challengeToken?: string }>("POST", "/auth/login", { body: { email, password } });
export const logout = () => request<{ ok: boolean }>("POST", "/auth/logout", { body: {} });
export const changePassword = (currentPassword: string, newPassword: string, code?: string) =>
  request<{ ok: boolean; reauthRequired: boolean; sessionsRevoked: number; keysRevoked: number }>(
    "POST", "/auth/password", { body: { currentPassword, newPassword, ...(code ? { code } : {}) } });
export const getMe = () =>
  request<{ userId: string; displayName: string; isOperator: boolean; isKey: boolean; canChangePassword: boolean; totpEnabled: boolean; totpPending: boolean; recoveryCodesRemaining: number }>("GET", "/me");

// ---- second factor (TOTP) ----
export interface TOTPStatus { enabled: boolean; pending: boolean; recoveryCodesRemaining: number }
export interface TOTPEnrollmentView { secret: string; otpauthUrl: string; recoveryCodes: string[] }
export const getTOTPStatus = () => request<TOTPStatus>("GET", "/me/totp");
export const enrollTOTP = (password: string) =>
  request<TOTPEnrollmentView>("POST", "/auth/totp/enroll", { body: { password } });
export const confirmTOTP = (code: string) => request<TOTPStatus>("POST", "/auth/totp/confirm", { body: { code } });
export const disableTOTP = (password: string, code: string) =>
  request<{ ok: boolean }>("POST", "/auth/totp/disable", { body: { password, code } });
export const regenerateRecoveryCodes = (password: string, code: string) =>
  request<{ recoveryCodes: string[] }>("POST", "/auth/totp/recovery-codes", { body: { password, code } });
// Challenge completion is pre-auth: the challenge token authorizes it, not a cookie.
export const completeChallengeTOTP = (challengeToken: string, code: string) =>
  request<{ userId: string; displayName: string }>("POST", "/auth/challenge/totp", { body: { challengeToken, code } });
export const completeChallengeRecovery = (challengeToken: string, code: string) =>
  request<{ userId: string; displayName: string }>("POST", "/auth/challenge/recovery", { body: { challengeToken, code } });
export const adminResetTOTP = (id: string, password: string, code: string) =>
  request<{ ok: boolean }>("POST", `/admin/users/${encodeURIComponent(id)}/totp-reset`, { body: { password, code } });
export interface AdminUser {id:string;displayName:string;email?:string;disabled:boolean;isOperator:boolean;roleManaged:boolean;createdAt:string}
export interface AdminAuditEvent {id:string;actor:string;action:string;resource:string;outcome:string;details:string;createdAt:string}
export interface AdminAuthSettings {authMode:string;signupEnabled:boolean;oidcIssuer:string;oidcAllowedClaims:Record<string,string[]>|null;oidcAdminClaims:Record<string,string[]>|null;oidcAllowAllUsers:boolean;externalSessionMinutes:number}
export const adminSettings=()=>request<AdminAuthSettings>('GET','/admin/settings');
export const adminUsers=(q:string,offset=0)=>request<{users:AdminUser[];total:number;limit:number}>('GET','/admin/users',{query:{q,offset}});
export const adminUpdateUser=(id:string,body:{disabled?:boolean;isOperator?:boolean;revokeSessions?:boolean})=>request<{ok:boolean}>('PATCH','/admin/users/'+encodeURIComponent(id),{body});
export const adminAudit=(offset=0)=>request<{events:AdminAuditEvent[]}>('GET','/admin/audit',{query:{offset}});
export interface KeyRecord {
  id: string;
  label: string;
  scopes: string[] | null;
  spaceIds: string[];
  expiresAt?: string | null;
  revokedAt?: string | null;
  createdAt: string;
}
export const listKeys = () => request<{ keys: KeyRecord[] }>("GET", "/me/keys");
export const createKey = (label: string, grants: {spaceIds: string[]; scopes: string[]; expiresInDays: number}) =>
  request<KeyRecord & { secret: string }>("POST", "/me/keys", { body: { label, ...grants } });
export const revokeKey = (id: string) => request<{ ok: boolean }>("DELETE", `/me/keys/${encodeURIComponent(id)}`);

// ---- spaces ----
export const listSpaces = () => request<{ spaces: SpaceWithRole[] }>("GET", "/spaces");
export const createSpace = (name: string, kind: "personal" | "shared", storage?:VaultStorageConfig) =>
  request<{ space: { id: string } }>("POST", "/spaces", { body: { name, kind, storage } });

// ---- documents ----
export const listDocuments = async (space: string, includeTrashed = false) => {
  const data = await request<{ documents: DocMeta[] | null }>("GET", `/spaces/${space}/documents`, {
    query: { trashed: includeTrashed ? "true" : undefined },
  });
  return { documents: normList(data.documents) };
};
export async function loadDocumentPages(space:string,trashed:boolean,onPage:(documents:DocMeta[],total:number,done:boolean)=>void,signal:AbortSignal,initial:DocMeta[]=[]){
 let cursor:string|undefined=initial.at(-1)?.path,documents:DocMeta[]=initial;
 do{
  const page=await request<{documents:DocMeta[]|null;nextCursor?:string;total:number}>('GET',`/spaces/${space}/documents`,{signal,query:{limit:500,cursor,trashed:trashed?'true':undefined}});
  if(signal.aborted)return;
  documents=documents.concat(normList(page.documents));cursor=page.nextCursor;
  onPage(documents,page.total,!cursor);
  // Give rendering and input a turn between chunks, including cached responses.
  if(cursor)await new Promise(resolve=>setTimeout(resolve,0));
 }while(cursor&&!signal.aborted);
}
export interface VaultLoad {spaceId:string;phase:'queued'|'scanning'|'indexing'|'resolving'|'ready'|'error';completed:number;total:number;skipped:number;error?:string}
export const vaultLoad=(space:string)=>request<{load:VaultLoad|null}>('GET',`/spaces/${space}/load`);
export const retryVaultLoad=(space:string)=>request('POST',`/spaces/${space}/load/retry`,{body:{}});
export const getDocument = (space: string, doc: string) =>
  request<NoteContent>("GET", `/spaces/${space}/documents/${doc}`);
export const createDocument = (space: string, path: string, content: string, idempotencyKey?:string) =>
  request<DocMeta>("POST", `/spaces/${space}/documents`, { body: { path, content },idempotencyKey });
export const saveContent = (space: string, doc: string, markdown: string, ifMatch: string) =>
  request<{ revision: string; committedAt: string }>("PUT", `/spaces/${space}/documents/${doc}/content`, {
    body: { markdown },
    ifMatch,
  });
export const renameDocument = (space: string, doc: string, path: string, ifMatch: string) =>
  request<{ ok: boolean }>("POST", `/spaces/${space}/documents/${doc}/rename`, { body: { path }, ifMatch });
export const trashDocument = (space: string, doc: string, ifMatch: string) =>
  request<{ ok: boolean }>("POST", `/spaces/${space}/documents/${doc}/trash`, { ifMatch });
export const restoreDocument = (space: string, doc: string) =>
  request<{ ok: boolean }>("POST", `/spaces/${space}/documents/${doc}/restore`);
export const listRevisions = (space: string, doc: string) =>
  request<{ revisions: RevisionMeta[] }>("GET", `/spaces/${space}/documents/${doc}/revisions`);
export const getRevision = (space: string, doc: string, rev: string) =>
  request<{ markdown: string }>("GET", `/spaces/${space}/documents/${doc}/revisions/${rev}`);
export const getBacklinks = async (space: string, doc: string) => {
  const data = await request<{ backlinks: Backlink[] | null }>("GET", `/spaces/${space}/documents/${doc}/backlinks`);
  return { backlinks: normList(data.backlinks) };
};

// ---- space level ----
export const search = async (space: string, q: string, limit = 25) => {
  const data = await request<{ hits: SearchHit[] | null }>("GET", `/spaces/${space}/search`, { query: { q, limit } });
  return { hits: normList(data.hits) };
};
export const graph = async (space: string, limit = 300) => {
  const data = await request<Omit<GraphData, "nodes" | "edges"> & {
    nodes: GraphData["nodes"] | null;
    edges: GraphData["edges"] | null;
  }>("GET", `/spaces/${space}/graph`, { query: { limit } });
  return { ...data, nodes: normList(data.nodes), edges: normList(data.edges) };
};
export const graphAnalysis = (space: string, opts: {query?:string;scope?:"vault"|"note"|"folder";scopeId?:string;scopePath?:string;direction?:"in"|"out"|"both";depth?:number;limit?:number} = {}) => {
  const query = new URLSearchParams({scope:opts.scope??"vault",mode:"document",direction:opts.direction??"both",limit:String(opts.limit??1000)});
  if(opts.query)query.set("query",opts.query);
  if(opts.scopeId)query.append("scopeId",opts.scopeId);
  if(opts.scopePath)query.set("scopePath",opts.scopePath);
  if(opts.depth!==undefined)query.set("depth",String(opts.depth));
  return request<AnalysisGraph>("GET",`/spaces/${encodeURIComponent(space)}/graph/analysis?${query}`);
};
export const tags = async (space: string) => {
  const data = await request<{ tags: TagCount[] | null }>("GET", `/spaces/${space}/tags`);
  return { tags: normList(data.tags) };
};

export const listFolders = (space:string) => request<{folders:string[]}>('GET',`/spaces/${space}/folders`);
export const createFolder = (space:string,path:string) => request<{path:string}>('POST',`/spaces/${space}/folders`,{body:{path}});

// ---- folder rename / trash / restore ----
export interface FolderMoveResult { path:string; moved:number; failures?:string[]|null }
export interface FolderTrashEntry { kind?: "attachment"; trashId:string; path:string; trashedAt:string; docIds:string[]|null }
export interface FolderTrashResult { trashId?:string; path:string; trashed:number; restored?:number; conflicts?:string[]; failures?:string[] }
export const renameFolder = (space:string,path:string,newPath:string) =>
  request<FolderMoveResult>('POST',`/spaces/${space}/folders/rename`,{body:{path,newPath}});
export const trashFolder = (space:string,path:string) =>
  request<FolderTrashResult>('POST',`/spaces/${space}/folders/trash`,{body:{path}});
export const listFolderTrashes = async (space:string) => {
  const data = await request<{folders:FolderTrashEntry[]|null}>('GET',`/spaces/${space}/folders/trash`);
  return { folders: normList(data.folders) };
};
export const restoreFolder = (space:string,trashId:string) =>
  request<FolderTrashResult>('POST',`/spaces/${space}/folders/restore`,{body:{trashId}});

// ---- attachments ----
export interface AttachmentMeta { path:string; size:number; contentType:string }
export const attachmentURL = (space:string,path:string) =>
  BASE+`/spaces/${space}/attachments/`+path.split('/').map(encodeURIComponent).join('/');
export const listAttachments = async (space:string,prefix='') => {
  const data = await request<{attachments:AttachmentMeta[]|null}>('GET',`/spaces/${space}/attachments`,{query:{path:prefix||undefined}});
  return { attachments: normList(data.attachments) };
};
export async function uploadAttachment(space:string,path:string,data:Blob|ArrayBuffer,onlyIfAbsent=false):Promise<AttachmentMeta>{
  const res = await fetch(attachmentURL(space,path),{
    method:'PUT', credentials:'same-origin',
    headers:{'Content-Type':'application/octet-stream','X-Netsidian-Requested':'1',...(onlyIfAbsent?{'If-None-Match':'*'}:{})}, body:data,
  });
  const text = await res.text();
  const parsed = text ? JSON.parse(text) : null;
  if(!res.ok){ const e=parsed?.error??{}; throw new ApiError(res.status,e.code??'unknown',e.message??res.statusText,!!e.retryable); }
  return parsed as AttachmentMeta;
}

// ---- editable text files (.puml/.plantuml/.mmd/.svg) ----
export const textFileURL = (space:string,path:string) =>
  BASE+`/spaces/${space}/text-files/`+path.split('/').map(encodeURIComponent).join('/');
export async function getTextFile(space:string,path:string):Promise<{content:string;etag:string}>{
  const res = await fetch(textFileURL(space,path),{credentials:'same-origin',headers:{'X-Netsidian-Requested':'1'}});
  const text = await res.text();
  const parsed = text ? JSON.parse(text) : null;
  if(!res.ok){ const e=parsed?.error??{}; throw new ApiError(res.status,e.code??'unknown',e.message??res.statusText,!!e.retryable); }
  return {content:parsed.content as string, etag:parsed.etag as string};
}
export async function putTextFile(space:string,path:string,content:string,etag:string):Promise<{etag:string}>{
  const res = await fetch(textFileURL(space,path),{
    method:'PUT', credentials:'same-origin',
    headers:{'Content-Type':'text/plain; charset=utf-8','If-Match':etag,'X-Netsidian-Requested':'1'}, body:content,
  });
  const text = await res.text();
  const parsed = text ? JSON.parse(text) : null;
  if(!res.ok){ const e=parsed?.error??{}; throw new ApiError(res.status,e.code??'unknown',e.message??res.statusText,!!e.retryable); }
  return {etag:parsed.etag as string};
}

// ---------- phase 2: vault lifecycle and workspace state ----------

export const getWorkspace = (space: string) =>
  request<WorkspaceState>("GET", `/spaces/${encodeURIComponent(space)}/workspace`);

export const putWorkspace = (space: string, state: WorkspaceState) =>
  request<{ ok: boolean }>("PUT", `/spaces/${encodeURIComponent(space)}/workspace`, { body: state });

export const renameVault = (space: string, name: string) =>
  request<{ space: { id: string; name: string } }>("PUT", `/spaces/${encodeURIComponent(space)}`, { body: { name } });

export async function exportVault(space: string, name: string): Promise<void> {
  const res = await fetch(`${BASE}/spaces/${encodeURIComponent(space)}/export`, {
    headers: { "X-Netsidian-Requested": "1" },
    credentials: "same-origin",
  });
  if (!res.ok) {
    let message = "export failed";
    try {
      const e = (await res.json())?.error;
      if (e?.message) message = e.message;
    } catch { /* non-JSON failure body */ }
    throw new Error(message);
  }
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `${(name || "vault").replace(/[^a-zA-Z0-9_-]/g, "-")}-export.zip`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 5000);
}

export const graph2=(space:string)=>request<import('../lib/graph2Model').Graph2Data>('GET',`/spaces/${space}/graph2`);
export interface MemoEntry {id:string;path:string;title:string;revision:string;createdAt:string;updatedAt:string;tags:string[];preview:string}
export interface MemoResult {memos:MemoEntry[];total:number;tags:{tag:string;count:number}[];nextCursor?:string}
export const memos=(space:string,opts:{folder?:string;tag?:string;q?:string;from?:string;before?:string;cursor?:string;limit?:number})=>request<MemoResult>('GET',`/spaces/${space}/memos`,{query:opts,emptyQueryKeys:['folder']});

// ---- folder-backed kanban boards ----
export interface BoardSummary {path:string;title:string;columns:number}
export interface BoardColumn {id:string;label:string;folder:string;completion?:boolean;wipLimit?:number}
export interface BoardCard {id:string;path:string;title:string;revision:string;updatedAt:string;warnings?:string[]}
export interface BoardColumnView extends BoardColumn {cards:BoardCard[]}
export interface BoardCreateDefaults {column?:string;content?:string}
export interface BoardConfig {version:number;title:string;columns:BoardColumn[];order?:Record<string,string[]>;createDefaults?:BoardCreateDefaults}
export interface Board {path:string;title:string;revision:string;config:BoardConfig;columns:BoardColumnView[];overflow?:BoardCard[];warnings?:string[]}
export const listBoards=async(space:string)=>{const data=await request<{boards:BoardSummary[]|null}>('GET',`/spaces/${encodeURIComponent(space)}/boards`);return {boards:normList(data.boards)}};
export const getBoard=(space:string,path:string)=>request<Board>('GET',`/spaces/${encodeURIComponent(space)}/boards/board`,{query:{path}});
export const createBoard=(space:string,body:{path:string;title:string;template?:string;columns?:BoardColumn[]},idempotencyKey?:string)=>request<Board>('POST',`/spaces/${encodeURIComponent(space)}/boards`,{body,idempotencyKey});
export const updateBoardConfig=(space:string,path:string,body:{title?:string;columns:BoardColumn[];createDefaults?:BoardCreateDefaults},ifMatch:string)=>request<Board>('PUT',`/spaces/${encodeURIComponent(space)}/boards/config`,{query:{path},body,ifMatch});
export const createBoardTask=(space:string,boardPath:string,body:{columnId:string;title:string;content?:string},idempotencyKey?:string)=>request<BoardCard>('POST',`/spaces/${encodeURIComponent(space)}/boards/tasks`,{query:{path:boardPath},body,idempotencyKey});
export const boardMoveTask=(space:string,boardPath:string,body:{docId:string;columnId:string;fromPath:string},ifMatch:string)=>request<BoardCard>('POST',`/spaces/${encodeURIComponent(space)}/boards/move`,{query:{path:boardPath},body,ifMatch});
export const boardReorder=(space:string,boardPath:string,body:{columnId:string;orderedIds:string[]},ifMatch:string)=>request<Board>('POST',`/spaces/${encodeURIComponent(space)}/boards/reorder`,{query:{path:boardPath},body,ifMatch});

// Storage and native attachment use the same transport and error contract.
export const getVaultStorage=(id:string)=>request<VaultStorageConfig>('GET',`/spaces/${encodeURIComponent(id)}/storage`);
export const configureVaultStorage=(id:string,body:VaultStorageConfig)=>request<VaultStorageConfig>('PUT',`/spaces/${encodeURIComponent(id)}/storage`,{body});
export const attachNativeVault=()=>request<AttachedVault>('POST','/native/vaults/attach',{body:{},query:{background:'true'}});
export const attachLocalVault=(path:string)=>request<AttachedVault>('POST','/local/vaults',{body:{path},query:{background:'true'}});

export interface SharingUser {id:string;displayName:string;email?:string}
export interface VaultMember {membership:{id:string};userId:string;displayName:string;email?:string;role:'owner'|'editor'|'viewer'}
export interface VaultSharing {role:'owner'|'editor'|'viewer';members:VaultMember[];invitations?:{id:string;email:string;role:string;expiresAt:string;consumedAt?:string;revokedAt?:string}[]}
export const vaultSharing=(space:string)=>request<VaultSharing>('GET',`/spaces/${space}`);
export const sharingUsers=(space:string,q:string)=>request<{users:SharingUser[]}>('GET',`/spaces/${space}/sharing/users`,{query:{q}});
export const shareVault=(space:string,userId:string,role:string)=>request('POST',`/spaces/${space}/members`,{body:{userId,role}});
export const changeVaultRole=(space:string,member:string,role:string)=>request('PUT',`/spaces/${space}/members/${member}`,{body:{role}});
export const removeVaultMember=(space:string,member:string)=>request('DELETE',`/spaces/${space}/members/${member}`);
export const inviteToVault=(space:string,email:string,role:string)=>request<{inviteUrl:string}>('POST',`/spaces/${space}/invitations`,{body:{email,role}});
export const revokeVaultInvitation=(space:string,id:string)=>request('DELETE',`/spaces/${space}/invitations/${id}`);

export const lookupDocument=(space:string,path:string)=>request<Pick<DocMeta,'id'|'path'|'kind'|'currentRevision'|'updatedAt'>>('GET','/spaces/'+encodeURIComponent(space)+'/documents/lookup',{query:{path}});

export interface DesktopMCPConfig {enabled:boolean;port:number;allowUnauthenticated:boolean;readOnly:boolean;allowTrash:boolean;vaultIds:string[]}
export interface DesktopMCPStatus {config:DesktopMCPConfig;running:boolean;url:string;error?:string}
export const getDesktopMCP=()=>request<DesktopMCPStatus>('GET','/native/mcp');
export const setDesktopMCP=(config:DesktopMCPConfig)=>request<DesktopMCPStatus>('PUT','/native/mcp',{body:config});

export const trashAttachment = (space:string,path:string) =>
 request<{trashId:string;path:string;kind:"attachment"}>("DELETE",`/spaces/${space}/attachments/${path.split('/').map(encodeURIComponent).join('/')}`);
export const moveAttachment = (space:string,from:string,to:string) =>
 request<{ok:boolean}>("POST",`/spaces/${space}/attachments/move`,{body:{from,to}});
