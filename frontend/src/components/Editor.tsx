import { EditorState } from "@codemirror/state";
import { EditorView, keymap, drawSelection, highlightActiveLine } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { markdown } from "@codemirror/lang-markdown";
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching } from "@codemirror/language";
import { useEffect, useRef } from "react";

export type SaveState = "dirty" | "saving" | "saved" | "error";

interface Props {
  docKey: string; // change to re-initialise content
  initialText: string;
  onSave: () => Promise<boolean>; // returns true when a revision landed
  onChangeDirty: (dirty: boolean) => void;
  onSaveState: (s: SaveState) => void;
  onText?: (text: string) => void; // called with the current doc text on every change
}

const DEBOUNCE_MS = 750; // per requirements: 750ms coalescing window

export function Editor({ docKey, initialText, onSave, onChangeDirty, onSaveState, onText }: Props) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const timerRef = useRef<number | undefined>(undefined);
  const dirtyRef = useRef(false);
  const onSaveRef = useRef(onSave);
  onSaveRef.current = onSave;
  const dirtyCbRef = useRef(onChangeDirty);
  dirtyCbRef.current = onChangeDirty;
  const stateCbRef = useRef(onSaveState);
  stateCbRef.current = onSaveState;
  const textCbRef = useRef(onText);
  textCbRef.current = onText;

  useEffect(() => {
    const schedule = () => {
      window.clearTimeout(timerRef.current);
      timerRef.current = window.setTimeout(async () => {
        if (!dirtyRef.current) return;
        stateCbRef.current("saving");
        const ok = await onSaveRef.current();
        if (ok) {
          dirtyRef.current = false;
          dirtyCbRef.current(false);
          stateCbRef.current("saved");
        } else {
          stateCbRef.current("error");
        }
      }, DEBOUNCE_MS);
    };
    const forceSave = async () => {
      window.clearTimeout(timerRef.current);
      if (!dirtyRef.current) return true;
      stateCbRef.current("saving");
      const ok = await onSaveRef.current();
      if (ok) {
        dirtyRef.current = false;
        dirtyCbRef.current(false);
        stateCbRef.current("saved");
      } else stateCbRef.current("error");
      return ok;
    };
    const view = new EditorView({
      state: EditorState.create({
        doc: initialText,
        extensions: [
          history(),
          drawSelection(),
          highlightActiveLine(),
          bracketMatching(),
          markdown(),
          syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
          EditorView.lineWrapping,
          keymap.of([
            { key: "Mod-s", preventDefault: true, run: () => { void forceSave(); return true; } },
            indentWithTab,
            ...defaultKeymap,
            ...historyKeymap,
          ]),
          EditorView.updateListener.of((u) => {
            if (!u.docChanged) return;
            textCbRef.current?.(u.view.state.doc.toString());
            dirtyRef.current = true;
            dirtyCbRef.current(true);
            schedule();
          }),
        ],
      }),
      parent: hostRef.current ?? document.body,
    });
    viewRef.current = view;
    return () => {
      window.clearTimeout(timerRef.current);
      view.destroy();
      viewRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [docKey]);

  return <div className="editor-wrap" ref={hostRef} />;
}
