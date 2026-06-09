import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { RuntimeProvider } from "./lib/runtime";
import App from "./App";
import "./globals.css";

const qc = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={qc}>
      <RuntimeProvider>
        <App />
      </RuntimeProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
