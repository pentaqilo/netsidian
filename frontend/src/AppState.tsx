import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import * as api from "./api/client";
import type { SpaceWithRole, InstanceSettings } from "./api/types";

export interface AppState {
  ready: boolean;
  // loading is true while a refresh is in flight, so consumers can tell a
  // transient "still fetching my vaults" state apart from a genuine absence.
  loading: boolean;
  // spacesError holds a real vault-list failure (permission/network). It is
  // surfaced as an error rather than being mistaken for "no vaults".
  spacesError: string;
  me: { userId: string; displayName: string; isOperator: boolean; canChangePassword?: boolean; totpEnabled?: boolean; totpPending?: boolean; recoveryCodesRemaining?: number } | null;
  settings: InstanceSettings | null;
  spaces: SpaceWithRole[];
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
}

const Ctx = createContext<AppState>(null as unknown as AppState);
export const useApp = () => useContext(Ctx);

export function AppProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [loading, setLoading] = useState(true);
  const [spacesError, setSpacesError] = useState("");
  const [me, setMe] = useState<AppState["me"]>(null);
  const [settings, setSettings] = useState<AppState["settings"]>(null);
  const [spaces, setSpaces] = useState<SpaceWithRole[]>([]);
  const refreshSequence = useRef(0);
  const account = useRef<string | null>(null);

  const refresh = async () => {
    const sequence = ++refreshSequence.current;
    let who: AppState["me"] = null;
    setLoading(true);
    setSpacesError("");
    try {
      const st = await api.getSettings();
      if (sequence !== refreshSequence.current) return;
      setSettings(st);
      if (st.needsSetup) {
        setMe(null);
        setSpaces([]);
        account.current = null;
        return;
      }
      who = await api.getMe();
      const sp = await api.listSpaces();
      if (sequence !== refreshSequence.current) return;
      account.current = who.userId;
      setMe(who);
      setSpaces(sp.spaces || []);
    } catch (e) {
      if (sequence !== refreshSequence.current) return;
      if (e instanceof api.ApiError && e.status === 401) {
        account.current = null;
        setMe(null);
        setSpaces([]);
      } else {
        // Keep an existing workspace and its drafts alive on transient errors.
        // Never reuse another account's vault list after a failed sign-in refresh.
        if (who) {
          if (account.current !== who.userId) setSpaces([]);
          account.current = who.userId;
          setMe(who);
        }
        setSpacesError(e instanceof Error ? e.message : String(e));
      }
    } finally {
      if (sequence === refreshSequence.current) {
        setLoading(false);
        setReady(true);
      }
    }
  };

  const signOut = async () => {
    await api.logout();
    refreshSequence.current++;
    account.current = null;
    setMe(null);
    setSpaces([]);
    setSpacesError("");
    setLoading(false);
  };

  useEffect(() => {
    refresh().catch(() => setReady(true));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <Ctx.Provider value={{ ready, loading, spacesError, me, settings, spaces, refresh, signOut }}>{children}</Ctx.Provider>
  );
}
