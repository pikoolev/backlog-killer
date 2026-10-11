import {defineConfig} from '@playwright/test';
export default defineConfig({testDir:'e2e',use:{baseURL:'http://client.localhost:4174',trace:'retain-on-failure'}});
