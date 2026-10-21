import { lstat, readdir, realpath } from 'node:fs/promises';
import { isAbsolute, join, resolve } from 'node:path';
import { homedir } from 'node:os';
import { Failure } from './process.js';

export const expected = [
  { path: '.fit-agent.toml', type: 'file', owner: 'agent' },
  { path: 'fit-agent/ATHLETE-PROFILE.md', type: 'file', owner: 'agent' },
  { path: 'fit-agent/README.md', type: 'file', owner: 'agent' },
  { path: 'fit-agent/.gitignore', type: 'file', owner: 'agent' },
  ...['training-plan-coach', 'training-session-coach', 'workout-builder'].map((name) => ({ path: `skills/${name}/SKILL.md`, type: 'file', owner: 'agent' })),
  ...['activities', 'wellness', 'planned-workouts', '.cache/activities', '.cache/wellness', '.cache/events'].map((dir) => ({
    path: `fit-agent/${dir}`, type: 'directory', owner: dir === 'planned-workouts' ? 'shared' : 'machine',
  })),
] as const;

export function workspacePath(configured?: string, active?: string): string {
  const value = configured ?? active;
  if (!value) throw new Failure('workspace', 'Set plugins.entries.fit-agent.config.workspace or use an active agent workspace.');
  const expanded = value.startsWith('~/') ? join(homedir(), value.slice(2)) : value;
  if (!isAbsolute(expanded)) throw new Failure('workspace', 'Workspace must be an absolute local path.');
  return resolve(expanded);
}

export async function exists(path: string): Promise<boolean> {
  try { await lstat(path); return true; } catch (err) {
    if ((err as NodeJS.ErrnoException).code === 'ENOENT') return false;
    throw new Failure('workspace', 'Cannot inspect workspace permissions.');
  }
}

export async function inspectWorkspace(path: string) {
  if (!await exists(path)) return { root: path, ready: false, expected: expected.map((e) => ({ ...e, exists: false })) };
  const root = await realpath(path);
  const uid = process.getuid?.();
  const gid = process.getgid?.();
  let inspected = 0;
  const check = async (path: string) => {
    const info = await lstat(path);
    if (++inspected > 50000) throw new Failure('workspace', 'Workspace exceeds the 50,000-entry inspection limit.');
    // A normal single-user Linux workspace often has umask-derived 664/775
    // permissions. Permit group write only for the current primary group; world
    // write, a foreign writable group, symlinks, special files, and foreign
    // owners remain unsafe because another principal could alter CLI inputs.
    const worldWritable = (info.mode & 0o002) !== 0;
    const foreignGroupWritable = (info.mode & 0o020) !== 0 && (gid === undefined || info.gid !== gid);
    if (info.isSymbolicLink() || (!info.isDirectory() && !info.isFile()) || worldWritable || foreignGroupWritable ||
      (uid !== undefined && info.uid !== uid && info.uid !== 0)) {
      throw new Failure('workspace', 'FitAgent paths must be regular, locally owned files/directories without world write access, foreign writable groups, or nested symlinks.');
    }
    return info;
  };
  const walk = async (path: string): Promise<void> => {
    const info = await check(path);
    if (info.isDirectory()) for (const name of await readdir(path)) await walk(join(path, name));
  };
  if (!(await check(root)).isDirectory()) throw new Failure('workspace', 'Workspace path is not a directory.');
  // Inspect metadata only, including cache descendants the CLI could write to.
  for (const dir of ['fit-agent', 'skills']) if (await exists(join(root, dir))) await walk(join(root, dir));
  const files = [];
  for (const e of expected) {
    const present = await exists(join(root, e.path));
    if (present) {
      const info = await check(join(root, e.path));
      if ((e.type === 'file') !== info.isFile()) throw new Failure('workspace', 'Workspace entry has the wrong file type.');
    }
    files.push({ ...e, exists: present });
  }
  return { root, ready: files.every((e) => e.exists), expected: files };
}
