import { expect, test } from 'bun:test';
import { mkdtemp, mkdir, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { gzipArchive } from '../build/archive.js';

async function run(args) {
  const child = Bun.spawn(args, { stdout: 'pipe', stderr: 'pipe' });
  const stderr = new Response(child.stderr).text();
  const status = await child.exited;
  if (status !== 0) throw new Error(await stderr);
  return new Response(child.stdout).text();
}

for (const [platform, level] of [['linux', 0], ['darwin', 9]]) {
  test(`${platform} archive supports tar -xzf with the intended compression level`, async () => {
    const directory = await mkdtemp(join(tmpdir(), 'yadirect-archive-'));
    try {
      const original = 'test executable content\n'.repeat(4096);
      const binary = join(directory, 'yadirect');
      const tar = join(directory, 'payload.tar');
      const archive = join(directory, `yadirect-1.0.0-${platform}-amd64.tar.gz`);
      const extracted = join(directory, 'extracted');
      await mkdir(extracted);
      await Bun.write(binary, original);
      await run(['tar', '-cf', tar, '-C', directory, 'yadirect']);
      await gzipArchive(tar, archive, level);
      expect([...new Uint8Array(await Bun.file(archive).arrayBuffer()).slice(0, 2)]).toEqual([0x1f, 0x8b]);
      if (level === 0) expect(Bun.file(archive).size).toBeGreaterThan(Bun.file(tar).size);
      else expect(Bun.file(archive).size).toBeLessThan(Bun.file(tar).size / 2);
      expect(await run(['tar', '-tzf', archive])).toBe('yadirect\n');
      await run(['tar', '-xzf', archive, '-C', extracted]);
      expect(await Bun.file(join(extracted, 'yadirect')).text()).toBe(original);
    } finally { await rm(directory, { recursive: true, force: true }); }
  });
}
