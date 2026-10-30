import { expect, test } from 'bun:test';
import { mkdtemp, mkdir, chmod, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { resolveUPX } from '../build/upx.js';

async function fixture(run) {
  const directory = await mkdtemp(join(tmpdir(), 'yadirect-upx-'));
  const executable = async (path, version) => {
    await Bun.write(path, `#!/bin/sh\nprintf 'upx ${version}\\n'\n`);
    await chmod(path, 0o755);
    return path;
  };
  try { await run(directory, executable); }
  finally { await rm(directory, { recursive: true, force: true }); }
}

test('UPX uses the pinned executable despite an older runner executable in PATH', async () => {
  await fixture(async (directory, executable) => {
    const system = join(directory, 'system'); await mkdir(system);
    await executable(join(system, 'upx'), '4.2.4');
    const cached = await executable(join(directory, 'pinned-upx'), '5.3.0');
    const code = `
      import { resolveUPX } from ${JSON.stringify(new URL('../build/upx.js', import.meta.url).href)};
      if (Bun.which('upx') !== ${JSON.stringify(join(system, 'upx'))}) throw new Error('Old runner executable missing');
      const resolved = await resolveUPX({ cached: ${JSON.stringify(cached)}, install: () => { throw new Error('Unexpected install'); } });
      if (resolved !== ${JSON.stringify(cached)}) throw new Error('Used the runner executable');
    `;
    const child = Bun.spawn([process.execPath, '-e', code], {
      env: { ...process.env, PATH: `${system}:${process.env.PATH}`, UPX_BINARY: '' },
      stdout: 'pipe', stderr: 'pipe',
    });
    const stderr = new Response(child.stderr).text();
    if (await child.exited !== 0) throw new Error(await stderr);
    await stderr;
  });
});

test('UPX installs the pinned executable when the cache is empty', async () => {
  await fixture(async (directory, executable) => {
    const cached = join(directory, 'upx'); let installs = 0;
    expect(await resolveUPX({ override: '', cached, install: async path => { installs++; await executable(path, '5.3.0'); } })).toBe(cached);
    expect(installs).toBe(1);
  });
});

test('UPX rejects old explicit overrides and accepts newer releases', async () => {
  await fixture(async (directory, executable) => {
    const override = join(directory, 'upx');
    for (const version of ['4.2.4', '5.2.9']) {
      await executable(override, version);
      await expect(resolveUPX({ override })).rejects.toThrow('UPX 5.3.0 or newer is required');
    }
    for (const version of ['5.3.0', '5.4.0', '6.0.0']) {
      await executable(override, version);
      expect(await resolveUPX({ override })).toBe(override);
    }
  });
});
