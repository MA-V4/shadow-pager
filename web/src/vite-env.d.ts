/// <reference types="vite/client" />

// The settings Vite reads from the environment when the app is built.
interface ImportMetaEnv {
  // The address of the Shadow-Pager API, or nothing to use the same address the page came from.
  readonly VITE_API_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
