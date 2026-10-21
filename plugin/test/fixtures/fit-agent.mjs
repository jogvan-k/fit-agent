#!/usr/bin/env node
// Fixture-only executable. No network and no real credentials.
import { readFileSync, writeFileSync, appendFileSync, mkdirSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
const root = dirname(fileURLToPath(import.meta.url));
const state = JSON.parse(readFileSync(join(root, 'fixture.json'), 'utf8'));
const args = process.argv.slice(2), cmd = args[0];
const option = (name) => args[args.indexOf(name) + 1];
const workspace = args.includes('--workspace') ? option('--workspace') : state.workspace;
appendFileSync(join(root, 'calls.jsonl'), JSON.stringify({ args, cwd: process.cwd(), secretEnvPresent: Boolean(process.env.FIT_AGENT_API_KEY || process.env.ICU_BASE_URL || process.env.NODE_OPTIONS) }) + '\n');
const emit = (data) => console.log(JSON.stringify({ schemaVersion: 1, ok: true, data }));
if (cmd === '--version') { console.log(state.version ?? '0.1.0'); process.exit(0); }
if (cmd === 'integration-info') {
  emit({ cliVersion: state.version ?? '0.1.0', protocol: state.protocol ?? 1,
    features: state.features ?? ['doctor-json', 'status-json', 'fetch-json', 'guarded-sync-v1', 'workspace-assertion'] });
  process.exit(0);
}
if (state.command === cmd) {
  if (state.mode === 'hang') { process.on('SIGTERM', () => {}); setInterval(() => {}, 1000); await new Promise(() => {}); }
  if (state.mode === 'flood') { await new Promise((resolve) => process.stdout.write('x'.repeat(300000), resolve)); process.exit(0); }
  if (state.mode === 'invalid') { console.log('{not json'); process.exit(0); }
  if (state.mode === 'missing') { emit({}); process.exit(0); }
  if (state.mode === 'unexpected') { emit({ rawCache: 'private-canary' }); process.exit(0); }
  if (state.mode === 'error') {
    console.error('API_KEY=private-canary');
    console.log(JSON.stringify({ schemaVersion: 1, ok: false, error: { layer: state.layer ?? 'network', message: 'private-canary' } }));
    process.exit(1);
  }
}
if (cmd === 'doctor') emit({ workspace: state.wrongWorkspace ?? workspace, profile: args.includes('--profile') ? option('--profile') : 'default', timezone: 'Europe/Copenhagen', credentialsReady: true });
if (cmd === 'init') {
  if (!process.stdin.isTTY || !process.stdout.isTTY) process.exit(2);
  console.log('Fixture protected prompt; no credential input required.');
  if (!args.includes('--dry-run')) for (const entry of state.expected) {
    const path = join(workspace, entry.path);
    mkdirSync(entry.type === 'directory' ? path : dirname(path), { recursive: true, mode: 0o755 });
    if (entry.type === 'file' && !existsSync(path)) writeFileSync(path, 'fixture\n', { mode: 0o644 });
  }
}
if (cmd === 'fetch') emit({ from: '2026-01-01', to: '2026-03-31', cache: { Added: 3, Updated: 0, Unchanged: 1, Skipped: 0, Errors: 0 }, render: { Added: 3, Updated: 0, Unchanged: 1, Errors: 0 } });
if (cmd === 'status') emit({ workspace, latestActivityDate: '2026-03-31', latestWellnessMonth: '2026-03', dataModifiedAt: '2026-03-31T12:00:00Z',
  plannedWorkoutCount: 2, upcomingDates: ['2026-04-01', '2026-04-02'], athleteProfileExists: true, trainingPlanExists: false });
if (cmd === 'sync-workouts') {
  const from = option('--from'), to = option('--to'), dryRun = args.includes('--dry-run');
  const path = join(workspace, 'fit-agent/planned-workouts/2026-04-01.md');
  const digest = createHash('sha256').update(JSON.stringify([from, to, state.remoteRevision ?? 0, existsSync(path) ? readFileSync(path, 'utf8') : ''])).digest('hex');
  if (!dryRun && (option('--expect-preview') !== digest || state.rejectPush)) {
    console.log(JSON.stringify({ schemaVersion: 1, ok: false, error: { layer: 'sync conflict', message: 'stale' } })); process.exit(1);
  }
  emit({ from, to: state.badRange ? '2099-01-01' : to, dryRun: state.badDryRun ? !dryRun : dryRun, fingerprint: digest,
    actions: state.actions ?? [{ kind: state.conflict ? 'conflict' : 'create', date: from, eventId: dryRun ? 0 : 123 }] });
  if (!dryRun) appendFileSync(join(root, 'writes.jsonl'), JSON.stringify({ from, to }) + '\n');
}
if (state.command === cmd && state.mode === 'exit') process.exit(1);
