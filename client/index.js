import { fileURLToPath } from 'node:url';
import { mkdir, rename } from 'node:fs/promises';
import { minify } from 'html-minifier-terser';

const root = new URL('../', import.meta.url);
export async function buildPage() {
  const result = await Bun.build({
    entrypoints: [fileURLToPath(new URL('client/index.html', root))],
    compile: true,
    target: 'browser',
    minify: { identifiers: true, syntax: true, whitespace: true },
    sourcemap: 'none',
  });
  if (!result.success) throw new AggregateError(result.logs, 'Frontend build failed');
  if (result.outputs.length !== 1) throw new Error('Expected one standalone HTML artifact');
  const html = await minify(await result.outputs[0].text(), {
    collapseWhitespace: true,
    removeComments: true,
    removeRedundantAttributes: true,
    collapseBooleanAttributes: true,
    removeEmptyAttributes: true,
    useShortDoctype: true,
  });
  const directory = new URL('public/', root);
  await mkdir(directory, { recursive: true });
  const temporary = new URL('.index.html.tmp', directory);
  await Bun.write(temporary, html);
  await rename(temporary, new URL('index.html', directory));
  const embeddedDirectory = new URL('server/public/', root);
  await mkdir(embeddedDirectory, { recursive: true });
  const embeddedTemporary = new URL('.index.html.tmp', embeddedDirectory);
  await Bun.write(embeddedTemporary, html);
  await rename(embeddedTemporary, new URL('index.html', embeddedDirectory));
  return html;
}

if (import.meta.main) {
  const html = await buildPage();
  console.log(`Built public/index.html (${Buffer.byteLength(html).toLocaleString('en-US')} bytes)`);
}
