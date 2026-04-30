import { mount } from 'svelte';
import App from './app.svelte';
import './styles/app.css';

const target = document.getElementById('app');
if (!target) {
  throw new Error('mount target #app not found');
}

const app = mount(App, { target });

export default app;
