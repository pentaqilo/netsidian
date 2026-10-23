export interface Space {
  id: string;
  slug: string;
  name: string;
  kind: "personal" | "shared";
  storageBinding?: string;
  archivedAt?: string | null;
}

export interface SpaceWithRole {
  space: Space;
  role: "owner" | "editor" | "viewer";
}

export interface DocMeta {
  tags?: string[];
  id: string;
  path: string;
  kind: string;
  title: string;
  currentRevision: string;
  updatedAt: string;
  tombstoneAt?: string | null;
}

export interface NoteContent {
  markdown: string;
  revision: string;
  canEdit: boolean;
  kind: string;
  path: string;
  title: string;
}

export interface RevisionMeta {
  id: string;
  predecessor?: string;
  sha256: string;
  byteLength: number;
  creator: string;
  createdAt: string;
}

export interface SearchHit {
  documentId: string;
  path: string;
  title: string;
  snippet: string;
  score: number;
}

export interface Backlink {
  documentId: string;
  path: string;
  title: string;
  snippet: string;
}

export interface GraphData {
  nodes: { id: string; path: string; title: string; links: number }[];
  edges: { source: string; target: string }[];
  total: number;
  truncated: boolean;
}

export interface AnalysisGraph {
  version: number;
  status: string;
  query: { scope: string; scopeIds?: string[]; scopePath?: string; mode: string; direction: string; depth: number; limit: number };
  nodes: { id: string; kind: "document" | "unresolved"; label: string; documentId?: string; path?: string; tags?: string[]; importance: number; metric: string }[];
  edges: { id: string; source: string; target: string; type: string; weight: number; evidence?: { documentId: string; path: string; revision: string; locator?: string; excerpt?: string; method: string; stale?: boolean }[] }[];
  matched: { nodes: number; edges: number };
  unresolvedMatched: { nodes: number; edges: number };
  returned: { nodes: number; edges: number };
  limits: { nodes: number; edges: number };
  truncated: boolean;
  truncationReason?: string;
}

export interface TagCount {
  tag: string;
  count: number;
}

export interface Memory {
  id: string;
  spaceId: string;
  documentId: string;
  kind: string;
  title: string;
  status: "proposed" | "accepted" | "rejected" | "superseded" | "expired";
  createdAt: string;
  updatedAt: string;
  content?: string;
  sources?: { documentId: string }[];
}

export interface ApiErrorBody {
  error: {
    code: string;
    message: string;
    retryable: boolean;
    currentRevision?: string;
  };
}

// Phase 2: per-user, per-vault workspace restoration state (P2-V04).
export interface WorkspaceState {
  openDocs: string[];
  activeDoc: string;
  expandedFolders: string[];
  paneWidths?: Record<string, number>;
  graphScope?: Record<string, unknown>;
  custom?: Record<string, unknown>;
}

/** Runtime facts come from the composition root, rather than URL/404 probes. */
export interface RuntimeCapabilities {
 kind:'desktop'|'web';
 auth:{passwordLogin:boolean;oauth:boolean;logout:boolean};
 storage:{defaultProvider:string;providers:string[];folderPicker:'native'|'server'|'none'};
}
export interface InstanceSettings {
 authMode:'none'|'local'|'external'|'mixed';needsSetup:boolean;signupEnabled:boolean;version:string;releaseDate?:string;
 features:Record<string,boolean>;runtime:RuntimeCapabilities;
}
export interface VaultStorageConfig {
 provider:string;bucket?:string;region?:string;endpoint?:string;prefix?:string;pathStyle?:boolean;
 auth?:string;accessKey?:string;secretKey?:string;sessionToken?:string;stsEndpoint?:string;roleArn?:string;
}
export type AttachedVault = {cancelled:true;space?:never;documents?:never;loading?:never}|{cancelled?:false;space:Space;documents:number;loading?:boolean};
