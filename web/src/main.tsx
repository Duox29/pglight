import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './index.css'
import { CommandProvider } from './shortcuts/ShortcutProvider'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <CommandProvider>
      <App />
    </CommandProvider>
  </StrictMode>,
)
