// Launched inside a real PTY by init.test.ts; never receives a credential.
import { writeFile } from 'node:fs/promises';
import { Bridge } from '../src/bridge.js';
const [binary, workspace, output, mode] = process.argv.slice(2);
if (!binary || !workspace || !output) throw new Error('missing fixture paths');
const bridge = new Bridge({ binaryPath: binary, workspace });
const session = { workspace, scope: 'fresh-install' };
const results: unknown[] = [];
results.push(await bridge.call('doctor', {}, session));
results.push(await bridge.call('init', { dryRun: mode === 'dry-run' }, session));
if (mode === 'full') {
  results.push(await bridge.call('doctor', {}, session));
  results.push(await bridge.call('refresh', {}, session));
  const range = { from: '2026-04-01', to: '2026-04-07' };
  const { outputs } = await import('../src/schemas.js');
  const preview = outputs.preview_workout_sync.parse(await bridge.call('preview_workout_sync', range, session));
  results.push(preview);
  if (preview.ok) results.push(await bridge.call('sync_workouts', { ...range, confirmationToken: preview.data.confirmationToken, confirmed: true }, session));
}
await writeFile(output, JSON.stringify(results));
