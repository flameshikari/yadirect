import { watch } from 'node:fs';
import { compile } from './compile.js';
import { fileURLToPath } from 'node:url';

await compile({ compress: false });
const launch = () => Bun.spawn([fileURLToPath(new URL('../yadirect', import.meta.url)), ...process.argv.slice(2)], { stdin: 'inherit', stdout: 'inherit', stderr: 'inherit' });
let server = launch();
let timer;
let building = false;
let dirty = false;
let stopping = false;
async function rebuild() {
  if (stopping) return;
  if (building) { dirty = true; return; }
  building = true;
  try {
    await compile({ compress: false });
    server.kill('SIGTERM');
    await server.exited;
    if (!stopping) server = launch();
  } catch (error) {
    console.error(error);
  } finally {
    building = false;
    if (dirty) { dirty = false; void rebuild(); }
  }
}
const onChange = () => {
  clearTimeout(timer);
  timer = setTimeout(rebuild, 100);
};
const watchers = [watch(new URL('../client/', import.meta.url), { recursive: true }, onChange), watch(new URL('../server/', import.meta.url), (event, file) => { if (file?.endsWith('.go')) onChange(); })];
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, async () => {
  stopping = true;
  for (const watcher of watchers) watcher.close();
  clearTimeout(timer);
  server.kill('SIGTERM');
  await server.exited;
  process.exit(0);
});
