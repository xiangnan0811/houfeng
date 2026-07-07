import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'

import './styles/reset.css'
import './index.css'

import { router } from './app/router'
import { AppBoot } from './app/AppBoot'
import { AuthProvider } from './lib/auth-context'
import { ThemeProvider } from './lib/theme-context'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <AuthProvider>
        <AppBoot />
        <RouterProvider router={router} />
      </AuthProvider>
    </ThemeProvider>
  </StrictMode>,
)
