import { createRoot } from 'react-dom/client';
import { ThemeProvider } from '@ui5/webcomponents-react';

import '@ui5/webcomponents/dist/Assets.js';
import '@ui5/webcomponents-fiori/dist/Assets.js';
import '@ui5/webcomponents-icons/dist/AllIcons.js';

import './index.css';
import App from './App';

const container = document.getElementById('root');
if (!container) {
  throw new Error('Root element not found');
}

createRoot(container).render(
  <ThemeProvider>
    <App />
  </ThemeProvider>
);
