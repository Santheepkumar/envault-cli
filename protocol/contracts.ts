import { z } from "zod";

export const PROTOCOL_VERSION = 1 as const;
export const MAX_PLAINTEXT = 1024 * 1024;
export const environments = ["dev", "staging", "prod"] as const;
export type Environment = (typeof environments)[number];
export const environmentSchema = z.enum(environments);
export const idSchema = z.uuid();
// New collection endpoints use a stable descending UUID cursor. History keeps
// its existing numeric version cursor; cursors are never interchangeable.
export const pageSchema = z.strictObject({
  before: idSchema.optional(),
  limit: z.coerce.number().int().min(1).max(100).default(50),
});
export const accountCredentialSchema = z.strictObject({
  id: idSchema,
  label: z.string(),
  createdAt: z.string(),
  expiresAt: z.string(),
  revokedAt: z.string().nullable(),
});
export const accountCredentialsSchema = z.strictObject({
  credentials: z.array(accountCredentialSchema),
  nextCursor: idSchema.nullable(),
});
export type AccountCredential = z.infer<typeof accountCredentialSchema>;
export const recoveryKitSchema = z.strictObject({
  v: z.literal(1),
  apiUrl: z.url().refine((value) => {
    const url = new URL(value);
    return (
      !url.username &&
      !url.password &&
      url.pathname === "/" &&
      !url.search &&
      !url.hash &&
      (url.protocol === "https:" ||
        (url.protocol === "http:" &&
          ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)))
    );
  }, "Recovery kit must use an HTTPS installation origin or local development origin"),
  userId: idSchema,
  recoveryKey: z.string().regex(/^[A-Za-z0-9+/]{43}=$/),
});
export type RecoveryKit = z.infer<typeof recoveryKitSchema>;
const base64 = z
  .string()
  .max(1_400_000)
  .regex(/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/);
const bytes = (length: number) =>
  base64.refine((v) => atob(v).length === length, `Expected ${length} bytes`);
export const boxSchema = z.strictObject({
  v: z.literal(1),
  nonce: bytes(12),
  ciphertext: base64.refine((v) => {
    const n = atob(v).length;
    return n >= 16 && n <= MAX_PLAINTEXT + 16;
  }),
});
export type Box = z.infer<typeof boxSchema>;
const keyBoxSchema = boxSchema.extend({ ciphertext: bytes(48) });
export const wrapSchema = z.strictObject({
  ...keyBoxSchema.shape,
  ephemeralPublicKey: bytes(32),
});
export type KeyWrap = z.infer<typeof wrapSchema>;
export const accountSchema = z.strictObject({
  v: z.literal(1),
  publicKey: bytes(32),
  salt: bytes(16),
  kdf: z.strictObject({
    name: z.literal("argon2id"),
    memory: z.literal(65536),
    iterations: z.literal(3),
    parallelism: z.literal(4),
  }),
  passwordWrap: keyBoxSchema,
  recoveryWrap: keyBoxSchema,
  privateKey: keyBoxSchema,
});
export type AccountEnvelope = z.infer<typeof accountSchema>;
export const keyContextSchema = z.strictObject({
  projectId: idSchema,
  environment: environmentSchema,
  epoch: z.number().int().min(1).max(2147483647),
});
export type KeyContext = z.infer<typeof keyContextSchema>;
const confirmationState = z.strictObject({
  epoch: z.number().int().positive(),
  accessRevision: z.number().int().positive(),
});
export const confirmationSchema = z.strictObject({
  dev: confirmationState,
  staging: confirmationState,
  prod: confirmationState,
});
export type OwnershipConfirmation = z.infer<typeof confirmationSchema>;
export const secretContextSchema = keyContextSchema.extend({
  revisionId: idSchema,
});
export type SecretContext = z.infer<typeof secretContextSchema>;
export const recipientWrapSchema = z.strictObject({
  recipientId: z.string().min(1).max(128),
  wrap: wrapSchema,
});
export const pushSchema = z.strictObject({
  revisionId: idSchema,
  epoch: z.number().int().positive(),
  expectedHead: idSchema.nullable(),
  idempotencyKey: idSchema,
  box: boxSchema,
});
export type PushInput = z.infer<typeof pushSchema>;
export type Version = SecretContext & {
  version: number;
  box: Box;
  createdAt: string;
  createdBy: string;
};
export type EnvironmentState = KeyContext & {
  head: string | null;
  accessRevision: number;
  rotationPending: boolean;
  write: boolean;
};
export type Project = {
  id: string;
  name: string;
  role: "owner" | "admin" | "developer";
  environments: EnvironmentState[];
  storageBytes: number;
};
export type MachineBundle = {
  v: 1;
  apiUrl: string;
  token: string;
  privateKey: string;
  projectId: string;
  environment: Environment;
  id: string;
};
export type AccountSession = {
  account: AccountEnvelope;
  privateKey: Uint8Array;
  vaultKey: Uint8Array;
};
export function keyContext(c: KeyContext): string {
  return `envault:v1:key:${c.projectId}:${c.environment}:${c.epoch}`;
}
export function secretContext(c: SecretContext): string {
  return `envault:v1:secret:${c.projectId}:${c.environment}:${c.epoch}:${c.revisionId}`;
}
export function wrapContext(c: KeyContext, recipient: string): string {
  return `${keyContext(c)}:recipient:${recipient}`;
}
