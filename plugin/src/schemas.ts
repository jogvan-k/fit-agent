import { z } from 'zod';

// Explicitly reject control characters before passing paths to spawn/filesystem APIs.
// eslint-disable-next-line no-control-regex
const safeString = z.string().min(1).max(4096).regex(/^[^\x00-\x1f\x7f]+$/);
export const configSchema = z.strictObject({
  binaryPath: safeString.optional(),
  workspace: safeString.optional(),
  profile: z.string().regex(/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$/).optional(),
  timeoutMs: z.number().int().min(1000).max(600000).default(120000),
  diagnosticVerbosity: z.enum(['quiet', 'normal']).default('quiet'),
});
export type Config = z.infer<typeof configSchema>;
export const date = z.iso.date().refine((s) => s >= '2000-01-01' && s <= '2099-12-31');
export const rangeShape = { from: date, to: date };
export const rangeSchema = z.strictObject(rangeShape).refine(
  ({ from, to }) => from <= to && (Date.parse(to) - Date.parse(from)) / 86400000 < 90,
  'Range must be ordered and contain at most 90 inclusive calendar days',
);
export const inputs = {
  doctor: z.strictObject({}),
  init: z.strictObject({ dryRun: z.boolean().default(false) }),
  refresh: z.strictObject({ since: z.string().regex(/^[1-9][0-9]{0,2}d$/).default('90d') })
    .refine(({ since }) => Number(since.slice(0, -1)) <= 366, 'History must be 1d through 366d'),
  status: z.strictObject({}),
  preview_workout_sync: rangeSchema,
  sync_workouts: z.strictObject({ ...rangeShape, confirmationToken: z.string().regex(/^[a-f0-9]{64}$/), confirmed: z.literal(true) })
    .refine(({ from, to }) => rangeSchema.safeParse({ from, to }).success, 'Invalid bounded date range'),
};
export type Tool = keyof typeof inputs;
export const layerSchema = z.enum(['binary', 'workspace', 'profile/config', 'Intervals authentication', 'network', 'validation', 'sync conflict']);
export type Layer = z.infer<typeof layerSchema>;
export const errorSchema = z.strictObject({ ok: z.literal(false), error: z.strictObject({ layer: layerSchema, message: z.string().max(512) }) });
export const actionSchema = z.strictObject({ kind: z.enum(['create', 'update', 'delete', 'unchanged', 'skip', 'conflict']), date,
  eventId: z.number().int().min(0).max(Number.MAX_SAFE_INTEGER) });
export const cliSyncSchema = z.strictObject({ ...rangeShape, dryRun: z.boolean(), fingerprint: z.string().regex(/^[a-f0-9]{64}$/),
  actions: z.array(actionSchema).max(500) });
export const countsSchema = z.strictObject({ create: z.number().int().nonnegative(), update: z.number().int().nonnegative(),
  delete: z.number().int().nonnegative(), unchanged: z.number().int().nonnegative(), skip: z.number().int().nonnegative(), conflict: z.number().int().nonnegative() });
const cacheStats = z.strictObject({ Added: z.number().int().nonnegative(), Updated: z.number().int().nonnegative(),
  Unchanged: z.number().int().nonnegative(), Skipped: z.number().int().nonnegative(), Errors: z.number().int().nonnegative() });
export const cliFetchSchema = z.strictObject({ ...rangeShape, cache: cacheStats, render: cacheStats.omit({ Skipped: true }) });
export const cliDoctorSchema = z.strictObject({ workspace: safeString, profile: safeString, timezone: safeString, credentialsReady: z.literal(true) });
export const infoSchema = z.strictObject({ cliVersion: z.string().max(64), protocol: z.literal(1), features: z.array(z.string().max(64)).max(20) });
const success = <T extends z.ZodType>(data: T) => z.strictObject({ ok: z.literal(true), data });
const cli = <T extends z.ZodType>(data: T) => z.union([
  success(data).extend({ schemaVersion: z.literal(1) }), errorSchema.extend({ schemaVersion: z.literal(1) }),
]);
export const cliEnvelope = cli;
const expectedFile = z.strictObject({ path: z.string(), owner: z.enum(['agent', 'machine', 'shared']), type: z.enum(['file', 'directory']), exists: z.boolean() });
export const doctorData = z.strictObject({ binaryPath: safeString, cliVersion: z.string().max(64), workspace: safeString,
  ready: z.boolean(), profile: safeString, expected: z.array(expectedFile).max(40), nextAction: z.string().max(512) });
export const statusData = z.strictObject({ workspace: safeString, latestActivityDate: date.nullable(), latestWellnessMonth: z.string().regex(/^\d{4}-\d{2}$/).nullable(),
  dataModifiedAt: z.iso.datetime().nullable(), plannedWorkoutCount: z.number().int().nonnegative(), upcomingDates: z.array(date).max(30),
  athleteProfileExists: z.boolean(), trainingPlanExists: z.boolean() });
export const previewData = cliSyncSchema.extend({ fingerprint: z.string().regex(/^[a-f0-9]{64}$/),
  counts: countsSchema, confirmationToken: z.string().regex(/^[a-f0-9]{64}$/).nullable(), expiresAt: z.iso.datetime(),
  confirmationRequired: z.literal(true) });
export const outputs = {
  doctor: z.union([success(doctorData), errorSchema]),
  init: z.union([success(z.strictObject({ filesCreated: z.array(z.string()).max(40), dryRun: z.boolean() })), errorSchema]),
  refresh: z.union([success(cliFetchSchema), errorSchema]),
  status: z.union([success(statusData), errorSchema]),
  preview_workout_sync: z.union([success(previewData), errorSchema]),
  sync_workouts: z.union([success(cliSyncSchema.extend({ counts: countsSchema })), errorSchema]),
};
