import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {/* Real paths rather than hashes, which means the server has to serve
        index.html for unknown routes — nginx already does via try_files. */}
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
