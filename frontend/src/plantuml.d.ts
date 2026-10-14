declare module 'markdown-it-footnote' {
 const plugin: (state: unknown) => void;
 export default plugin;
}

// Untyped TeaVM-compiled PlantUML engine (@plantuml/core); API per GITHUB_INTEGRATION.md.
declare module '@plantuml/core/plantuml.js' {
  export function render(lines: string[], targetId: string, options?: { dark?: boolean }): void;
  export function renderToString(
    lines: string[],
    onSuccess: (svg: string) => void,
    onError: (message: string) => void,
  ): void;
}
