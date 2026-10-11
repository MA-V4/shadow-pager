import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import '@fontsource/ibm-plex-sans/latin-400.css'
import '@fontsource/ibm-plex-sans/latin-500.css'
import '@fontsource/ibm-plex-sans/latin-600.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'

import { App } from './App'
import './index.css'

const root = document.getElementById('root')
if (!root) {
  throw new Error('The page has no element with the id root to draw the app into.')
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
