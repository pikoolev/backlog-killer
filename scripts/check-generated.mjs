import {createHash} from 'node:crypto';
import {readFile} from 'node:fs/promises';
import {spawn} from 'node:child_process';

const path = new URL('../packages/contracts/src/generated.ts', import.meta.url);
const digest = async () => createHash('sha256').update(await readFile(path)).digest('hex');
const before = await digest();
await new Promise((resolve, reject) => {
  const child = process.env.npm_execpath ? spawn(process.execPath, [process.env.npm_execpath, 'generate'], {stdio: 'inherit'}) : spawn('pnpm', ['generate'], {stdio: 'inherit'});
  child.once('error', reject);
  child.once('exit', (code) => code === 0 ? resolve() : reject(new Error(`generator exited ${code}`)));
});
const after = await digest();
if (before !== after) {
  throw new Error('generated TypeScript contract was stale; commit the regenerated file');
}
