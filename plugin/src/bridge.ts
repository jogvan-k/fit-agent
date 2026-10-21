import { createHash, randomBytes } from 'node:crypto';
import { z } from 'zod';
import { cliDoctorSchema, cliEnvelope, cliFetchSchema, cliSyncSchema, configSchema, infoSchema, inputs, outputs, statusData } from './schemas.js';
import type { Config, Tool, Layer } from './schemas.js';
import { Failure, parseVersion, discoverBinary, Runner } from './process.js';
import { inspectWorkspace, workspacePath } from './workspace.js';

export const PREVIEW_TTL_MS = 10 * 60 * 1000;
const features = ['doctor-json', 'status-json', 'fetch-json', 'guarded-sync-v1', 'workspace-assertion'];
type Preview = { fingerprint: string; cliFingerprint: string; from: string; to: string; expires: number; scope: string };
export type Session = { workspace?: string; scope: string; assertCurrent?: () => void };

export function fingerprint(range: { from: string; to: string }, version: string, workspace: string, profile: string, cliFingerprint: string): string {
  // The Go fingerprint contains the planned file bytes and full remote plan.
  return createHash('sha256').update(JSON.stringify([1, range.from, range.to, version, workspace, profile, cliFingerprint])).digest('hex');
}

export function counts(actions: z.infer<typeof cliSyncSchema>['actions']) {
  const result = { create: 0, update: 0, delete: 0, unchanged: 0, skip: 0, conflict: 0 };
  for (const action of actions) result[action.kind]++;
  return result;
}

export class Bridge {
  readonly config: Config;
  private readonly runner: Runner;
  private readonly previews = new Map<string, Preview>();
  private readonly busy = new Set<string>();
  constructor(config: unknown, private readonly now: () => number = Date.now) {
    const parsed = configSchema.safeParse(config ?? {});
    if (!parsed.success) throw new Failure('validation', 'Invalid FitAgent plugin configuration; check the manifest config schema.');
    this.config = parsed.data;
    this.runner = new Runner(this.config);
  }

  private async json<T extends z.ZodType>(binary: string, args: string[], workspace: string | undefined, schema: T, signal?: AbortSignal): Promise<z.infer<T>> {
    const result = await this.runner.run(binary, args, workspace, signal);
    let parsed: { ok: true; data: z.infer<T> } | { ok: false; error: { layer: Layer; message: string } };
    try { parsed = cliEnvelope(schema).parse(JSON.parse(result.stdout)) as typeof parsed; }
    catch { throw new Failure('validation', 'Unexpected fit-agent JSON or unsupported schema. Upgrade the CLI and plugin together.', { code: result.code, stderr: result.stderr }); }
    if (!parsed.ok) {
      // Never echo even schema-valid CLI error prose: errors are a possible
      // private-data channel. Only allowlisted categories cross the boundary.
      const remedies = {
        binary: 'Check fit-agent installation and binaryPath.', workspace: 'Check workspace shape, ownership, and selected profile.',
        'profile/config': 'Run fit-agent init in a private local terminal; never paste credentials in chat.',
        'Intervals authentication': 'Reconfigure credentials with fit-agent init in a private local terminal.',
        network: 'Check network access to Intervals.icu and retry the preview.', validation: 'Check local workout files and CLI compatibility in a private terminal.',
        'sync conflict': 'Preview is stale or workouts conflict. Reconcile manually, preview again, and confirm the new result.',
      };
      throw new Failure(parsed.error.layer, remedies[parsed.error.layer], { code: result.code, stderr: result.stderr });
    }
    if (result.code !== 0) throw new Failure('validation', 'fit-agent reported success with a failing exit code.', { code: result.code, stderr: result.stderr });
    return parsed.data;
  }

  async call(tool: Tool, args: unknown, session: Session, signal?: AbortSignal): Promise<unknown> {
    let lock: string | undefined;
    try {
      const parsed = inputs[tool].safeParse(args);
      if (!parsed.success) throw new Failure('validation', 'Invalid tool arguments. Use the declared schema; sync needs an ordered range of at most 90 days and explicit matching confirmation.');
      const binary = await discoverBinary(this.config.binaryPath);
      const versionResult = await this.runner.run(binary, ['--version'], undefined, signal);
      if (versionResult.code !== 0) throw new Failure('binary', 'fit-agent --version failed. Check the installed executable.');
      const version = parseVersion(versionResult.stdout);
      const info = await this.json(binary, ['integration-info', '--json'], undefined, infoSchema, signal);
      if (parseVersion(info.cliVersion) !== version || features.some((feature) => !info.features.includes(feature))) {
        throw new Failure('binary', 'CLI lacks the version-one plugin protocol. Run brew upgrade fit-agent; see plugin/README.md for a source build.');
      }
      const shape = await inspectWorkspace(workspacePath(this.config.workspace, session.workspace));
      const workspace = shape.root;
      if (this.busy.has(workspace)) throw new Failure('workspace', 'A FitAgent operation is already running for this workspace. Retry after it finishes.');
      this.busy.add(workspace); lock = workspace;
      const profileArgs = this.config.profile ? ['--profile', this.config.profile] : [];
      const command = (name: string, extra: string[] = []) => [name, '--json', '--workspace', workspace, ...profileArgs, ...extra];
      if (tool === 'init') {
        const { dryRun } = inputs.init.parse(args);
        const initArgs = ['init', '--workspace', workspace, ...(this.config.profile ? ['--profile-name', this.config.profile] : []), ...(dryRun ? ['--dry-run'] : [])];
        try {
          // Direct terminal descriptors only. Neither prompt input nor terminal
          // output travels through the agent tool, logger, or process capture.
          session.assertCurrent?.();
          const result = await this.runner.run(binary, initArgs, undefined, signal, true);
          if (result.code !== 0) throw new Failure('profile/config', 'Interactive fit-agent init failed. See the private local terminal.');
        } catch (err) {
          if (err instanceof Failure && err.layer === 'profile/config') {
            // JSON argv is actionable without suggesting unsafe shell quoting.
            throw new Failure('profile/config', `Run this argument vector in a private local terminal: ${JSON.stringify([binary, ...initArgs])}. Never paste credentials in chat.`.slice(0, 512));
          }
          throw err;
        }
        const after = await inspectWorkspace(workspace);
        const filesCreated = after.expected.filter((e) => e.type === 'file' && e.exists && !shape.expected.find((old) => old.path === e.path)?.exists).map((e) => e.path);
        return outputs.init.parse({ ok: true, data: { filesCreated, dryRun } });
      }
      if (tool === 'doctor' && !shape.ready) {
        return outputs.doctor.parse({ ok: true, data: { binaryPath: binary, cliVersion: version, workspace,
          ready: false, profile: this.config.profile ?? 'CLI default', expected: shape.expected,
          nextAction: 'Run fit_agent_init from a local terminal session, or fit-agent init --workspace <configured-workspace> in a private terminal.' } });
      }
      if (!shape.ready) throw new Failure('workspace', 'Workspace is incomplete. Run fit_agent_doctor and interactive fit-agent init.');
      const doctor = await this.json(binary, command('doctor'), workspace, cliDoctorSchema, signal);
      if (doctor.workspace !== workspace || (this.config.profile && doctor.profile !== this.config.profile)) throw new Failure('workspace', 'CLI selected a different workspace or profile. Correct plugin configuration.');
      if (tool === 'doctor') return outputs.doctor.parse({ ok: true, data: { binaryPath: binary, cliVersion: version, workspace,
        ready: true, profile: doctor.profile, expected: shape.expected, nextAction: 'Ready. Run fit_agent_refresh to update training data.' } });
      if (tool === 'status') {
        const data = await this.json(binary, command('status'), workspace, statusData, signal);
        if (data.workspace !== workspace) throw new Failure('workspace', 'CLI status workspace mismatch.');
        return outputs.status.parse({ ok: true, data });
      }
      if (tool === 'refresh') {
        const { since } = inputs.refresh.parse(args);
        session.assertCurrent?.();
        const data = await this.json(binary, command('fetch', ['--since', since]), workspace, cliFetchSchema, signal);
        if (data.cache.Errors || data.render.Errors) throw new Failure('validation', 'Fetch reported partial failures. Inspect the CLI locally.');
        return outputs.refresh.parse({ ok: true, data });
      }
      const { from, to } = parsed.data as { from: string; to: string };
      const rangeArgs = ['--from', from, '--to', to];
      const scope = JSON.stringify([session.scope, workspace, doctor.profile, binary]);
      let approved: Preview | undefined;
      // Prune expired tokens and cap memory. Tokens live only in this plugin
      // instance and cannot be replayed across sessions or gateway restarts.
      for (const [token, preview] of this.previews) if (preview.expires <= this.now()) this.previews.delete(token);
      if (tool === 'sync_workouts') {
        const { confirmationToken } = inputs.sync_workouts.parse(args);
        approved = this.previews.get(confirmationToken);
        this.previews.delete(confirmationToken); // Consume before any remote operation, even failure.
        if (!approved || approved.scope !== scope || approved.from !== from || approved.to !== to) {
          throw new Failure('sync conflict', 'Confirmation is missing, expired, already used, or does not match this session and range. Preview again.');
        }
      }
      const data = await this.json(binary, command('sync-workouts', [...rangeArgs, '--dry-run']), workspace, cliSyncSchema, signal);
      if (!data.dryRun || data.from !== from || data.to !== to || data.actions.some((a) => a.date < from || a.date > to)) {
        throw new Failure('validation', 'CLI preview range or dry-run contract mismatch.');
      }
      const digest = fingerprint({ from, to }, version, workspace, doctor.profile, data.fingerprint);
      if (tool === 'preview_workout_sync') {
        const expires = this.now() + PREVIEW_TTL_MS;
        const token = counts(data.actions).conflict ? null : randomBytes(32).toString('hex');
        if (this.previews.size >= 100) this.previews.delete(this.previews.keys().next().value!);
        if (token) this.previews.set(token, { fingerprint: digest, cliFingerprint: data.fingerprint, from, to, expires, scope });
        return outputs.preview_workout_sync.parse({ ok: true, data: { ...data, fingerprint: digest, counts: counts(data.actions),
          confirmationToken: token, expiresAt: new Date(expires).toISOString(), confirmationRequired: true } });
      }
      if (!approved || approved.expires <= this.now() || approved.fingerprint !== digest || counts(data.actions).conflict) {
        throw new Failure('sync conflict', 'Workspace or remote plan changed. Present a fresh preview and obtain confirmation again.');
      }
      session.assertCurrent?.();
      const synced = await this.json(binary, command('sync-workouts', [...rangeArgs, '--expect-preview', approved.cliFingerprint]), workspace, cliSyncSchema, signal);
      if (synced.dryRun || synced.from !== from || synced.to !== to || synced.fingerprint !== data.fingerprint || synced.actions.some((a) => a.kind === 'conflict' || a.date < from || a.date > to)) {
        throw new Failure('validation', 'CLI returned an unexpected sync result. Inspect the remote calendar before retrying.');
      }
      return outputs.sync_workouts.parse({ ok: true, data: { ...synced, counts: counts(synced.actions) } });
    } catch (err) {
      const safe = err instanceof Failure ? err : new Failure('workspace', 'FitAgent could not inspect the local workspace or validate the result. Check it in a private terminal.');
      return { ok: false, error: { layer: safe.layer, message: safe.message } };
    } finally { if (lock) this.busy.delete(lock); }
  }
}
