import { mount } from 'svelte';
import App from './app/App.svelte';
import '@fontsource/material-symbols-outlined/400.css';
import './app/tailwind.css';
import './app/global.css';

const app = mount(App, { target: document.getElementById('app')! });

export default app;
