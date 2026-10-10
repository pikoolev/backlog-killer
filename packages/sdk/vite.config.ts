import {defineConfig} from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig(({mode})=>({plugins:[react()],build:{lib:{entry:'src/index.tsx',name:'BacklogKiller',formats:mode==='iife'?['iife']:['es'],fileName:()=>mode==='iife'?'backlog-killer.js':'index.js'},rollupOptions:{output:{inlineDynamicImports:true}}}}));
