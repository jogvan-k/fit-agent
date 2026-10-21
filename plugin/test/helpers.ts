import { mkdtemp, copyFile, chmod, writeFile, mkdir, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import type { TestContext } from 'node:test';
import { expected } from '../src/workspace.js';

export async function fixture(t: TestContext, ready = true) {
  const root = await mkdtemp(join(tmpdir(), 'fit-agent-plugin-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const workspace = join(root, 'workspace with spaces;literal');
  const binary = join(root, 'fit-agent');
  await copyFile(new URL('./fixtures/fit-agent.mjs', import.meta.url), binary);
  await chmod(binary, 0o755);
  const state: Record<string, unknown> = { workspace, expected };
  const save = () => writeFile(join(root, 'fixture.json'), JSON.stringify(state));
  await save();
  if (ready) for (const entry of expected) {
    const path = join(workspace, entry.path);
    await mkdir(entry.type === 'directory' ? path : dirname(path), { recursive: true, mode: 0o755 });
    if (entry.type === 'file') await writeFile(path, 'fixture\n', { mode: 0o644 });
  }
  const calls = async (): Promise<{ args: string[]; secretEnvPresent: boolean; cwd: string }[]> => (await readFile(join(root, 'calls.jsonl'), 'utf8')).trim().split('\n').filter(Boolean).map((s) => JSON.parse(s));
  return { root, workspace, binary, state, save, calls, session: { workspace, scope: 'test-session' } };
}
