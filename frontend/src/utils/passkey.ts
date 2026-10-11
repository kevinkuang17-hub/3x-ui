// WebAuthn helpers: the server speaks base64url JSON, the browser API wants ArrayBuffers.

type Json = Record<string, unknown>;

function fromB64url(value: string): ArrayBuffer {
  const b64 = value.replace(/-/g, '+').replace(/_/g, '/');
  const padded = b64 + '='.repeat((4 - (b64.length % 4)) % 4);
  const bin = atob(padded);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out.buffer;
}

function toB64url(buf: ArrayBuffer | null | undefined): string {
  if (!buf) return '';
  const bytes = new Uint8Array(buf);
  let bin = '';
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function withBuffers(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(list)) return undefined;
  return list.map((d: Json) => ({
    ...(d as object),
    id: fromB64url(d.id as string),
  })) as PublicKeyCredentialDescriptor[];
}

export function passkeysSupported(): boolean {
  const host = window.location.hostname;
  const isIp = /^[\d.]+$/.test(host) || host.includes(':');
  return typeof window.PublicKeyCredential !== 'undefined' && window.isSecureContext && !isIp;
}

export async function createPasskey(options: Json): Promise<Json> {
  const pk = options.publicKey as Json;
  const user = pk.user as Json;
  const publicKey = {
    ...pk,
    challenge: fromB64url(pk.challenge as string),
    user: { ...user, id: fromB64url(user.id as string) },
    excludeCredentials: withBuffers(pk.excludeCredentials),
  } as unknown as PublicKeyCredentialCreationOptions;
  const cred = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null;
  if (!cred) throw new Error('cancelled');
  const res = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: toB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: toB64url(res.clientDataJSON),
      attestationObject: toB64url(res.attestationObject),
      transports: typeof res.getTransports === 'function' ? res.getTransports() : [],
    },
  };
}

export async function getPasskey(options: Json): Promise<Json> {
  const pk = options.publicKey as Json;
  const publicKey = {
    ...pk,
    challenge: fromB64url(pk.challenge as string),
    allowCredentials: withBuffers(pk.allowCredentials),
  } as unknown as PublicKeyCredentialRequestOptions;
  const cred = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential | null;
  if (!cred) throw new Error('cancelled');
  const res = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: toB64url(cred.rawId),
    type: cred.type,
    response: {
      clientDataJSON: toB64url(res.clientDataJSON),
      authenticatorData: toB64url(res.authenticatorData),
      signature: toB64url(res.signature),
      userHandle: toB64url(res.userHandle),
    },
  };
}
