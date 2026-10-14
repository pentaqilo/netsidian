import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { AppProvider } from "./AppState";
import App from "./App";
import "./styles.css";
import { watchBrandFavicon } from './lib/brand';

const stopFavicon = watchBrandFavicon();
if (import.meta.hot) import.meta.hot.dispose(stopFavicon);

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing #root element");

createRoot(rootEl).render(
  <StrictMode>
    <BrowserRouter>
      <AppProvider>
        <App />
      </AppProvider>
    </BrowserRouter>
  </StrictMode>,
);
