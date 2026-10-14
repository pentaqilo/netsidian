import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { cpSync, mkdirSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, resolve } from 'node:path';
import type { Plugin } from 'vite';

// Keep CMaps, fonts and codecs local; PDF conversion never needs a CDN.
function pdfAssets(): Plugin {
 let output='';
 return {name:'netsidian-pdf-assets',apply:'build',
  configResolved(config){output=resolve(config.root,config.build.outDir,'pdfjs')},
  closeBundle(){
   const library=dirname(createRequire(import.meta.url).resolve('pdfjs-dist/package.json'));
   mkdirSync(output,{recursive:true});
   for(const dir of ['cmaps','standard_fonts','wasm'])cpSync(resolve(library,dir),resolve(output,dir),{recursive:true});
  }
 };
}
const devAPI = process.env.NETSIDIAN_DEV_API || "http://127.0.0.1:3479";

// The Go server embeds ../dist; keep output there.
export default defineConfig({
  plugins: [react(), pdfAssets()],
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
  server: {
    proxy: {
      "/api": { target: devAPI, changeOrigin: true, headers: { Origin: devAPI } },
    },
  },
});
